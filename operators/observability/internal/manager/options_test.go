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
