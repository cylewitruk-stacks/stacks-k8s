// Package protocolcontracts verifies pinned sBTC sources and renders the direct signer manager.
package protocolcontracts

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"text/template"
)

// contractPins contains metadata only; the upstream contract sources remain external.
//
//go:embed sbtc-contracts.json
var contractPins []byte

// managerTemplate owns the direct-staking manager's implementation, independently of bridge contracts.
//
//go:embed templates/direct-signer.clar.tmpl
var managerTemplate string

// Source binds one deployment to its exact reviewed upstream bytes.
type Source struct {
	// Name is the published contract name.
	Name string `json:"name"`
	// SHA256 pins the unmodified upstream source.
	SHA256 string `json:"sha256"`
	// ClarityVersion selects the publication language version.
	ClarityVersion int `json:"clarityVersion"`
	// Source is loaded from the external verified bundle.
	Source string `json:"-"`
}

// Load checks every source before bootstrap claims any mutation authority.
func Load(directory string) ([]Source, error) {
	var pins struct {
		Contracts []Source `json:"contracts"`
	}
	if err := json.Unmarshal(contractPins, &pins); err != nil {
		return nil, err
	}
	if directory == "" {
		return nil, fmt.Errorf("contract deployment requires the pinned checkout's contracts/contracts directory")
	}
	for i := range pins.Contracts {
		item := &pins.Contracts[i]
		path := filepath.Join(directory, item.Name+".clar")
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("read pinned contract %s: %w", path, err)
		}
		data, err := io.ReadAll(io.LimitReader(file, 128*1024+1))
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return nil, fmt.Errorf("read pinned contract %s: %w", path, err)
		}
		if len(data) == 0 || len(data) > 128*1024 || fmt.Sprintf("%x", sha256.Sum256(data)) != item.SHA256 {
			return nil, fmt.Errorf("pinned contract checksum mismatch: %s", path)
		}
		item.Source = string(data)
	}
	return pins.Contracts, nil
}

// DirectManager renders the only holder accepted by this non-custodial manager.
func DirectManager(holder string) (string, error) {
	if !regexp.MustCompile(`^ST[0-9A-HJKMNP-TV-Z]{26,39}$`).MatchString(holder) {
		return "", fmt.Errorf("invalid direct holder principal")
	}
	t, err := template.New("manager").Parse(managerTemplate)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	if err = t.Execute(&out, holder); err != nil {
		return "", err
	}
	return out.String(), nil
}

// Pins returns the public pinned-source metadata without upstream source bytes.
func Pins() []byte { return append([]byte(nil), contractPins...) }
