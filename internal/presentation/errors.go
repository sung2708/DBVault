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
	"github.com/sung2708/DBVault/internal/update"
)

func (r *Renderer) Error(operation, usage, help string, err error) {
	r.ErrorTo(r.err, operation, usage, help, err)
}
func (r *Renderer) ErrorTo(w io.Writer, operation, usage, help string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clearLocked()
	if r.options.JSON {
		record := map[string]any{"level": "error", "operation": r.safe(operation), "error": r.redactor.Text(err.Error()), "exit_code": fault.ExitCode(err), "help": help}
		var checkFailure *update.CheckError
		if errors.As(err, &checkFailure) {
			record["state"] = checkFailure.Result.State
			record["installed_version"] = checkFailure.Result.InstalledVersion
		}
		json.NewEncoder(w).Encode(record)
		return
	}
	var checkFailure *update.CheckError
	if errors.As(err, &checkFailure) {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(w, r.status("warning", "Update check canceled", true))
			return
		}
		fmt.Fprintln(w, r.status("error", "Could not check for updates", true))
		if checkFailure.Result.InstalledVersion != "" {
			fmt.Fprintf(w, "  Installed    %s\n", r.safe(checkFailure.Result.InstalledVersion))
		}
		fmt.Fprintf(w, "  Reason       %s\n\nTry again later.\n", Sanitize(r.redactor.Text(checkFailure.Result.Reason)))
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
	if cause != nil && cause.Kind == fault.Dependency {
		fmt.Fprintln(w, "\nRun dbvault doctor to locate missing native tools. Install the client tools matching your database version, then set database.tools paths or add them to PATH.")
	}
	fmt.Fprintf(w, "\nUsage:\n  %s\n\nRun %q for usage and examples.\n", usage, help)
	if !r.options.Verbose && cause != nil && cause.Kind != fault.Configuration && cause.Kind != fault.Unsupported {
		fmt.Fprintln(w, "Use --verbose for full diagnostic details.")
	}
}
