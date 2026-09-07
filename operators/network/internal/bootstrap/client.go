// Package bootstrap implements bounded external provisioning and PoX-4 maintenance.
// It is never imported by an operator controller.
package bootstrap

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// Options selects one explicit environment and its external SDK adapter.
type Options struct {
	Manifest, Kubeconfig, Context, SDKDirectory, Evidence string
	BitcoinPort, StacksPort                               int
}

// session owns all subprocesses and bounded HTTP access for one bootstrap invocation.
type session struct {
	options    Options
	namespace  string
	forwarders []*exec.Cmd
	http       *http.Client
	events     []map[string]any
	evidence   os.FileInfo
}

// kube executes a bounded command against only the selected kubeconfig and namespace.
func (s *session) kube(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	prefix := []string{"--kubeconfig", s.options.Kubeconfig, "--context", s.options.Context, "--namespace", s.namespace, "--request-timeout=20s"}
	cmd := exec.CommandContext(ctx, "kubectl", append(prefix, args...)...)
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("kubectl %s failed", args[0])
	}
	return data, nil
}

// get decodes one current Kubernetes object without including raw data in errors.
func (s *session) get(ctx context.Context, resource, name string, out any) error {
	data, err := s.kube(ctx, "get", resource, name, "-o", "json")
	if err != nil {
		return err
	}
	if json.Unmarshal(data, out) != nil {
		return fmt.Errorf("invalid %s response", resource)
	}
	return nil
}

// patch applies an external bootstrap change to the declared parent spec.
func (s *session) patch(ctx context.Context, name string, spec any) error {
	data, err := json.Marshal(map[string]any{"spec": spec})
	if err != nil {
		return err
	}
	_, err = s.kube(ctx, "patch", "stacksnetwork", name, "--type=merge", "-p", string(data))
	return err
}

// wait polls read-only prerequisites within an explicit deadline.
func wait(ctx context.Context, label string, seconds int, check func() bool) error {
	timer := time.NewTimer(time.Duration(seconds) * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if check() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return fmt.Errorf("%s did not complete", label)
		case <-ticker.C:
		}
	}
}

// forward starts a loopback-only port-forward and retains process ownership until cleanup.
func (s *session) forward(ctx context.Context, service string, local, remote int) error {
	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", s.options.Kubeconfig, "--context", s.options.Context, "--namespace", s.namespace, "port-forward", "--address", "127.0.0.1", "service/"+service, strconv.Itoa(local)+":"+strconv.Itoa(remote))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		return err
	}
	s.forwarders = append(s.forwarders, cmd)
	ready := make(chan struct{})
	var once sync.Once
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if bytes.Contains(scanner.Bytes(), []byte("Forwarding from")) {
				once.Do(func() { close(ready) })
			}
		}
	}()
	select {
	case <-ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(20 * time.Second):
		return fmt.Errorf("port-forward startup timed out")
	}
}

// close terminates and reaps every forwarding process, including failed startups.
func (s *session) close() {
	for _, cmd := range s.forwarders {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
}

// request performs one bounded HTTP operation; redirects and mutation retries are disabled.
func (s *session) request(ctx context.Context, method, url string, body []byte, user, password, contentType string, seconds int) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if user != "" {
		req.SetBasicAuth(user, password)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	response, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed; do not retry an uncertain mutation")
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil || len(data) >= 4<<20 {
		return nil, fmt.Errorf("HTTP request did not acknowledge success (status %d)", response.StatusCode)
	}
	if response.StatusCode != http.StatusOK {
		return nil, &httpResponseError{status: response.StatusCode, body: data}
	}
	return data, nil
}

// httpResponseError retains a bounded response for endpoint-specific decoding only.
type httpResponseError struct {
	status int
	body   []byte
}

// Error deliberately excludes arbitrary server details.
func (e *httpResponseError) Error() string {
	return fmt.Sprintf("HTTP request did not acknowledge success (status %d)", e.status)
}

// record persists only public evidence, with owner-only file permissions.
func (s *session) record(event string, details map[string]any) error {
	value := map[string]any{"event": event, "at": time.Now().UTC().Format(time.RFC3339Nano)}
	for k, v := range details {
		value[k] = v
	}
	s.events = append(s.events, value)
	data, err := json.MarshalIndent(map[string]any{"schemaVersion": 1, "namespace": s.namespace, "events": s.events}, "", "  ")
	if err != nil {
		return err
	}
	if err = s.writeEvidence(append(data, '\n')); err != nil {
		return err
	}
	public, _ := json.Marshal(details)
	fmt.Println(event, string(public))
	return nil
}

// claimEvidence reserves a new operation-specific file before any external mutation.
func (s *session) claimEvidence(operation string) error {
	if s.options.Evidence == "" {
		s.options.Evidence = s.options.Manifest + "." + operation + "-evidence.json"
	}
	path := s.options.Evidence
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("claim evidence %q (use a fresh path for each invocation): %w", path, err)
	}
	s.evidence, err = file.Stat()
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return s.record("EvidenceStarted", map[string]any{"operation": operation})
}

// writeEvidence atomically replaces only this invocation's snapshot after flushing it.
func (s *session) writeEvidence(data []byte) error {
	path := s.options.Evidence
	current, err := os.Lstat(path)
	if err != nil || s.evidence == nil || !os.SameFile(current, s.evidence) {
		return fmt.Errorf("evidence %q is not owned by this invocation", path)
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".bootstrap-evidence-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err = file.Write(data); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = os.Rename(file.Name(), path); err != nil {
		return err
	}
	s.evidence = info
	return nil
}
