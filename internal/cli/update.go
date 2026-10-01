package cli

import (
	"context"
	"errors"

	"github.com/spf13/cobra"
	"github.com/sung2708/DBVault/internal/fault"
	"github.com/sung2708/DBVault/internal/update"
)

func (o *options) updateCommand() *cobra.Command {
	parent := &cobra.Command{
		Use: "update", Short: "Check official DBVault releases",
		Long:    "Update-related commands only check official release information and explain how to update. DBVault never downloads or installs an update.",
		GroupID: "other", Args: noPositionalArgs,
		Example: "  dbvault update check\n  dbvault update check --output json",
	}
	check := &cobra.Command{
		Use: "check", Short: "Check whether a newer stable release is available",
		Long:    "Compare this installation with the latest official stable DBVault release. This command reads release metadata only; it never downloads or installs anything.",
		Example: "  dbvault update check\n  dbvault update check --output json\n  dbvault update check --no-color",
		Args:    noPositionalArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			force, _ := c.Flags().GetBool("force")
			result, err := o.updates.Check(c.Context(), o.build.Version, force)
			if err != nil {
				failure := &update.CheckError{Result: result, Err: err}
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return failure
				}
				err = fault.Wrap(fault.Connection, "official release lookup", failure)
				return o.redactor.Error(err)
			}
			return o.output(c, result)
		},
	}
	check.Flags().Bool("force", false, "Bypass the 24-hour release metadata cache")
	parent.AddCommand(check)
	return parent
}
