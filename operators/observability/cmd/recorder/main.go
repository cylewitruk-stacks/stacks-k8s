// Command recorder runs a namespace-scoped passive recording workload.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	observation "github.com/cylewitruk-stacks/stacks-k8s/operators/observability/api/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/observability/internal/recorder"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/observability/internal/telemetry"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/metadata"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func main() {
	var namespace, name, uid string
	var generation int64
	flag.StringVar(&namespace, "namespace", "", "Recording namespace.")
	flag.StringVar(&name, "telemetry", "", "NetworkTelemetry name.")
	flag.StringVar(&uid, "uid", "", "Exact NetworkTelemetry UID.")
	flag.Int64Var(&generation, "generation", 0, "Exact admitted generation.")
	flag.Parse()
	if err := run(ctrl.SetupSignalHandler(), namespace, name, uid, generation); err != nil {
		fmt.Fprintln(os.Stderr, "recorder stopped:", err)
		os.Exit(1)
	}
}

// run validates the immutable worker binding before opening any source subscriptions.
func run(ctx context.Context, namespace, name, uid string, generation int64) error {
	config, err := ctrl.GetConfig()
	if err != nil {
		return err
	}
	config.QPS = 10
	config.Burst = 20
	scheme, err := recorderScheme()
	if err != nil {
		return err
	}
	c, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return err
	}
	t := &observation.NetworkTelemetry{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, t); err != nil {
		return err
	}
	if string(t.UID) != uid || generation != t.Generation || !t.Status.Admitted || !t.DeletionTimestamp.IsZero() {
		return fmt.Errorf("recording admission does not match worker identity")
	}
	d, err := dynamic.NewForConfig(config)
	if err != nil {
		return err
	}
	m, err := metadata.NewForConfig(config)
	if err != nil {
		return err
	}
	exporter, err := recorder.NewExporter(os.Getenv("GREPTIME_ENDPOINT"), os.Getenv("GREPTIME_AUTH"),
		telemetry.TablePrefix(t)+"_objects", t.Spec.Retention.Window)
	if err != nil {
		return err
	}
	r := recorder.Recorder{
		Metadata:  m,
		Dynamic:   d,
		Client:    c,
		Telemetry: t,
		PodUID:    types.UID(os.Getenv("POD_UID")),
		Sink:      exporter,
	}
	return r.Run(ctx)
}

// recorderScheme includes every typed resource used by recording and collector health.
func recorderScheme() (*runtime.Scheme, error) {
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{observation.AddToScheme, corev1.AddToScheme} {
		if err := add(scheme); err != nil {
			return nil, err
		}
	}
	return scheme, nil
}
