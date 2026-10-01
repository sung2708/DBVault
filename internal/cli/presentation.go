package cli

import (
	"bytes"
	"context"
	"os"

	"github.com/spf13/cobra"
	"github.com/sung2708/DBVault/internal/presentation"
	"github.com/sung2708/DBVault/internal/security"
)

type displayKey struct{}
type displayBinding struct {
	command  *cobra.Command
	renderer *presentation.Renderer
}

func globalBool(c *cobra.Command, name string) bool {
	v, _ := c.Root().PersistentFlags().GetBool(name)
	return v
}
func jsonMode(c *cobra.Command) bool {
	mode, _ := c.Root().PersistentFlags().GetString("output")
	return globalBool(c, "json") || mode == "json"
}
func displayOptions(c *cobra.Command, redactor *security.Redactor) presentation.Options {
	return presentation.Options{JSON: jsonMode(c), Quiet: globalBool(c, "quiet"), NoColor: globalBool(c, "no-color"), Verbose: globalBool(c, "verbose"), DisableAnimation: c.Root().Annotations["dbvault.scheduled"] == "true", Redactor: redactor}
}
func (o *options) display(c *cobra.Command) *presentation.Renderer {
	if ctx := c.Context(); ctx != nil {
		if binding, ok := ctx.Value(displayKey{}).(displayBinding); ok && binding.command == c {
			return binding.renderer
		}
	}
	r := presentation.New(c.OutOrStdout(), c.ErrOrStderr(), displayOptions(c, o.redactor))
	ctx := c.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	c.SetContext(context.WithValue(ctx, displayKey{}, displayBinding{c, r}))
	return r
}
func installHelp(root *cobra.Command, o *options) {
	defaultHelp := root.HelpFunc()
	root.SetHelpFunc(func(c *cobra.Command, args []string) {
		out := c.OutOrStdout()
		var b bytes.Buffer
		c.SetOut(&b)
		defaultHelp(c, args)
		c.SetOut(out)
		r := presentation.New(out, c.ErrOrStderr(), displayOptions(c, o.redactor))
		// Help remains a human interface, even when a result-format flag is present.
		r.Help(b.String(), c == root)
	})
}
func validateOutput(c *cobra.Command, _ []string) error {
	mode, _ := c.Root().PersistentFlags().GetString("output")
	if mode != "text" && mode != "json" {
		return &outputError{}
	}
	if mode == "text" && c.Root().PersistentFlags().Changed("output") && globalBool(c, "json") {
		return &outputError{conflict: true}
	}
	return nil
}

type outputError struct{ conflict bool }

func (e *outputError) Error() string {
	if e.conflict {
		return "--json conflicts with --output text; use --output json"
	}
	return "--output must be text or json"
}
func errorDisplay(c *cobra.Command) *presentation.Renderer {
	if ctx := c.Context(); ctx != nil {
		if binding, ok := ctx.Value(displayKey{}).(displayBinding); ok && binding.command == c {
			return binding.renderer
		}
	}
	return presentation.New(c.OutOrStdout(), c.ErrOrStderr(), displayOptions(c, security.New(os.Getenv("DB_PASSWORD"), os.Getenv("AWS_SECRET_ACCESS_KEY"), os.Getenv("AWS_SESSION_TOKEN"), os.Getenv("AZURE_CLIENT_SECRET"))))
}
