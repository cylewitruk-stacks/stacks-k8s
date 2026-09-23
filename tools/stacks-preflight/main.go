// Command stacks-preflight reports read-only point-in-time experiment prerequisites.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"syscall"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// request selects exact cluster/network identity and explicit evidence requirements.
type request struct {
	Kubeconfig         string         `json:"kubeconfig"`
	Context            string         `json:"context"`
	Namespace          string         `json:"namespace"`
	Network            string         `json:"network"`
	NetworkUID         string         `json:"networkUID"`
	Telemetry          string         `json:"telemetry"`
	OperatorNamespace  string         `json:"operatorNamespace"`
	OperatorDeployment string         `json:"operatorDeployment"`
	Chaos              bool           `json:"chaos"`
	MaxAgeSeconds      int            `json:"maxAgeSeconds"`
	RequiredSources    []string       `json:"requiredSources,omitempty"`
	Backend            backendRequest `json:"backend"`
}

// checkStatus reports only the requested prerequisite, not experiment safety or recovery.
type checkStatus string

const (
	checkPass    checkStatus = "Pass"
	checkFail    checkStatus = "Fail"
	checkUnknown checkStatus = "Unknown"
)

// check preserves a timestamp and bounded public explanation for one independent read.
type check struct {
	Name       string      `json:"name"`
	Status     checkStatus `json:"status"`
	ObservedAt time.Time   `json:"observedAt"`
	Detail     string      `json:"detail"`
}

// report contains only public checks and requested network identity.
type report struct {
	NetworkUID string  `json:"networkUID"`
	Checks     []check `json:"checks"`
	Passed     bool    `json:"passed"`
}

func (r *report) add(name string, status checkStatus, detail string) {
	r.Checks = append(r.Checks, check{name, status, time.Now().UTC(), detail})
	if status != checkPass {
		r.Passed = false
	}
}

var (
	namePattern   = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)
	sourcePattern = regexp.MustCompile(`^[a-z][a-z0-9./-]{0,127}$`)
	uidPattern    = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Stdin, os.Stdout)
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, "stacks-preflight:", err)
		os.Exit(1)
	}
}

// run does no writes to Kubernetes or the backend and never follows backend redirects.
func run(ctx context.Context, input io.Reader, output io.Writer) error {
	var r request
	data, readErr := io.ReadAll(io.LimitReader(input, 65537))
	if readErr != nil || len(data) > 65536 {
		return errors.New("request exceeds byte bound or cannot be read")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&r) != nil || d.Decode(&struct{}{}) != io.EOF {
		return errors.New("expected one bounded request on stdin")
	}
	if err := r.validate(); err != nil {
		return err
	}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: r.Kubeconfig},
		&clientcmd.ConfigOverrides{CurrentContext: r.Context}).ClientConfig()
	if err != nil {
		return errors.New("could not load selected kube-context")
	}
	if err := validateAuth(config); err != nil {
		return err
	}
	config.Timeout = 10 * time.Second
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return errors.New("could not create Kubernetes client")
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	result := inspect(ctx, client, r)
	checkBackend(ctx, r, &result)
	if err := json.NewEncoder(output).Encode(result); err != nil {
		return err
	}
	if !result.Passed {
		return errors.New("one or more requested prerequisites failed or are unknown")
	}
	return nil
}

func (r request) validate() error {
	if r.Kubeconfig == "" || r.Context == "" || !uidPattern.MatchString(r.NetworkUID) || r.MaxAgeSeconds < 15 ||
		r.MaxAgeSeconds > 600 ||
		len(r.RequiredSources) > 24 {
		return errors.New("invalid cluster identity or freshness bounds")
	}
	for _, name := range []string{r.Namespace, r.Network, r.Telemetry, r.OperatorNamespace, r.OperatorDeployment} {
		if !namePattern.MatchString(name) {
			return errors.New("invalid resource name")
		}
	}
	for _, name := range r.RequiredSources {
		if !sourcePattern.MatchString(name) {
			return errors.New("invalid source name")
		}
	}
	return r.Backend.validate()
}

// validateAuth avoids unbounded external credential execution in this bounded CLI.
func validateAuth(config *rest.Config) error {
	if config.ExecProvider != nil || config.AuthProvider != nil {
		return errors.New("preflight requires static certificate/token credentials; auth plugins are unsupported")
	}
	return nil
}
