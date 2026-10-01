package presentation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/sung2708/DBVault/internal/fault"
)

func (r *Renderer) Error(operation, usage, help string, err error) {
	r.ErrorTo(r.err, operation, usage, help, err)
}
func (r *Renderer) ErrorTo(w io.Writer, operation, usage, help string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clearLocked()
	if r.options.JSON {
		json.NewEncoder(w).Encode(map[string]any{"level": "error", "operation": r.safe(operation), "error": r.redactor.Text(err.Error()), "exit_code": fault.ExitCode(err), "help": help})
		return
	}
	title := operation + " failed"
	if operation == "dbvault" {
		title = "Command failed"
	}
	if errors.Is(err, context.Canceled) {
		title = "Operation canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		title = "Operation timed out"
	}
	fmt.Fprintln(w, r.status("error", "Error: "+title, true))
	var cause *fault.Error
	if errors.As(err, &cause) {
		fmt.Fprintln(w, "  Stage    "+r.safe(cause.Op))
		fmt.Fprintln(w, "  Category "+r.safe(string(cause.Kind)))
	}
	detail := r.redactor.Text(err.Error())
	if !r.options.Verbose {
		detail = ansi.Truncate(detail, 600, "...")
	}
	for _, line := range strings.Split(detail, "\n") {
		fmt.Fprintln(w, "  "+Sanitize(line))
	}
	fmt.Fprintf(w, "\nUsage:\n  %s\n\nRun %q for usage and examples.\n", usage, help)
	if !r.options.Verbose && cause != nil && cause.Kind != fault.Configuration && cause.Kind != fault.Unsupported {
		fmt.Fprintln(w, "Use --verbose for full diagnostic details.")
	}
}
