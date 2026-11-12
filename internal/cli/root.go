// Package cli wires the doc-drift-checker command line interface.
package cli

import "github.com/spf13/cobra"

// NewRootCmd builds the root command. The version is injected by the caller so
// release builds can set it through linker flags.
func NewRootCmd(version string) *cobra.Command {
	root := &cobra.Command{
		Use:     "doc-drift-checker",
		Short:   "Report code changes whose README passage did not change",
		Version: version,
		Args:    cobra.NoArgs,
		// Errors are printed by the entry point, which also maps them to exit codes.
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	root.AddCommand(newLinksCmd(), newCheckCmd())
	return root
}
