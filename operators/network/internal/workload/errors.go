package workload

import (
	"errors"
	"fmt"
)

type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

// Permanent marks a deterministic workload error that requires a specification change.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{err: fmt.Errorf("invalid workload declaration: %w", err)}
}

// IsPermanent reports whether retrying without a new resource generation is futile.
func IsPermanent(err error) bool {
	var target *permanentError
	return errors.As(err, &target)
}
