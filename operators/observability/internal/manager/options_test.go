package manager

import (
	"flag"
	"testing"
)

func TestRetiredVersionFlagRejected(t *testing.T) {
	options := Options{}
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	options.Bind(flags)
	if err := flags.Parse([]string{"--network-api-version=v1alpha1"}); err == nil {
		t.Fatal("retired network reader requested successfully")
	}
}

func TestExplicitNamespaceBoundary(t *testing.T) {
	for _, tc := range []struct {
		value string
		valid bool
		count int
	}{
		{"", true, 1}, {"lab-a, lab-b", true, 2}, {"lab-a,", false, 0}, {"lab-a,lab-a", false, 0}, {"UPPER", false, 0},
	} {
		result, err := (Options{Namespace: tc.value, LeaderNamespace: "system"}).enrolledNamespaces()
		if (err == nil) != tc.valid || (err == nil && len(result) != tc.count) {
			t.Fatalf("%q: %v %v", tc.value, result, err)
		}
	}
}
