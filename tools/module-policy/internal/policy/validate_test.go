package policy

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		modules string
		wantErr bool
	}{
		{name: "minimal API graph", modules: "example.test/api\nk8s.io/api v0.36.4\nk8s.io/apimachinery v0.36.4\n"},
		{name: "controller runtime", modules: "sigs.k8s.io/controller-runtime v0.24.1\n", wantErr: true},
		{name: "client go", modules: "k8s.io/client-go v0.36.4\n", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := Validate(strings.NewReader(test.modules), []string{
				"k8s.io/client-go",
				"sigs.k8s.io/controller-runtime",
			})
			if (err != nil) != test.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %t", err, test.wantErr)
			}
		})
	}
}
