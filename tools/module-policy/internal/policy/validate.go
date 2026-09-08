// Package policy validates dependency boundaries for independently versioned modules.
package policy

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Validate rejects forbidden modules from a go list -m all result.
func Validate(input io.Reader, forbidden []string) error {
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		for _, denied := range forbidden {
			if fields[0] == denied || strings.HasPrefix(fields[0], denied+"/") {
				return fmt.Errorf("forbidden module dependency %s", fields[0])
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read module graph: %w", err)
	}
	return nil
}
