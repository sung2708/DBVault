// Package fault classifies operational errors without losing their causes.
package fault

import (
	"context"
	"errors"
	"fmt"
)

type Kind string

const (
	Configuration Kind = "configuration"
	Connection    Kind = "connection"
	Dependency    Kind = "dependency"
	Unsupported   Kind = "unsupported capability"
	Backup        Kind = "backup"
	Storage       Kind = "storage"
	Integrity     Kind = "integrity"
	Restore       Kind = "restore"
)

type Error struct {
	Kind Kind
	Op   string
	Err  error
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s: %v", e.Kind, e.Op, e.Err) }
func (e *Error) Unwrap() error { return e.Err }
func Wrap(k Kind, op string, err error) error {
	if err == nil {
		return nil
	}
	return &Error{k, op, err}
}
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return 5
	}
	var e *Error
	if errors.As(err, &e) {
		switch e.Kind {
		case Connection:
			return 2
		case Dependency:
			return 3
		case Integrity:
			return 4
		}
	}
	return 1
}
