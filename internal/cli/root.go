// Package cli wires the doc-drift-checker command line interface.
package cli

import "github.com/spf13/cobra"

// NewRootCmd builds the root command. The version is injected by the caller so
// release builds can set it through linker flags.
func NewRootCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:     "doc-drift-checker",
		Short:   "Report code changes whose README passage did not change",
		Version: version,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
}
