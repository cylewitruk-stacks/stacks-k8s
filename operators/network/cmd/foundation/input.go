package main

import (
	"fmt"
	"io"
	"os"

	participantworkload "github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/participantworkload"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/participantworkload/stacksconfig"
)

// publicInput reads a bounded public binding request without exposing its content in errors.
func publicInput(mode, inline, path string) (string, error) {
	if path == "" {
		if len(inline) > stacksconfig.MaximumBytes {
			return "", fmt.Errorf("public input exceeds size bound")
		}
		return inline, nil
	}
	if (mode != participantworkload.ModeResolveBitcoinConfig &&
		mode != participantworkload.ModeResolveStacksConfig &&
		mode != participantworkload.ModeValidateStacksConfig) ||
		inline != "" {
		return "", fmt.Errorf("--input-file requires a configuration resolver and cannot accompany --input")
	}
	// #nosec G304 -- Explicit CLI input path is intentionally caller-selected and read with a size limit.
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("public input file unavailable")
	}
	defer func() { _ = f.Close() }() // Read/cleanup completion cannot change the operation's result.
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > stacksconfig.MaximumBytes {
		return "", fmt.Errorf("public input file must be a nonempty bounded regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, stacksconfig.MaximumBytes+1))
	if err != nil || len(data) == 0 || len(data) > stacksconfig.MaximumBytes {
		return "", fmt.Errorf("public input file is unreadable or oversized")
	}
	return string(data), nil
}
