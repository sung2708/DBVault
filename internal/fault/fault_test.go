package fault

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestExitCodes(t *testing.T) {
	for _, tt := range []struct {
		err  error
		code int
	}{{nil, 0}, {errors.New("x"), 1}, {Wrap(Connection, "ping", errors.New("x")), 2}, {Wrap(Dependency, "tool", errors.New("x")), 3}, {Wrap(Integrity, "hash", errors.New("x")), 4}, {Wrap(Backup, "dump", fmt.Errorf("interrupted: %w", context.Canceled)), 5}} {
		if got := ExitCode(tt.err); got != tt.code {
			t.Errorf("got %d want %d", got, tt.code)
		}
	}
}
