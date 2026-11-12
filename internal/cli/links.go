package cli

import (
	"github.com/spf13/cobra"

	"github.com/JtheGunner/doc-drift-checker/internal/links"
)

func newLinksCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "links",
		Short: "Show which code files each documentation section describes",
		Long: "Show the mapping of documentation sections to tracked code files, " +
			"with the origin of each link (annotation, config, implicit), " +
			"references that match no tracked file, and unmapped sections.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			report, err := buildLinksReport()
			if err != nil {
				return err
			}
			if asJSON {
				return links.WriteJSON(cmd.OutOrStdout(), report)
			}
			return links.WriteText(cmd.OutOrStdout(), report)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the mapping as JSON")
	return cmd
}

// buildLinksReport resolves every configured documentation file against the tracked files.
func buildLinksReport() (links.Report, error) {
	in, err := loadRepoInputs()
	if err != nil {
		return links.Report{}, err
	}
	docs, err := in.resolveDocs(in.tracked)
	if err != nil {
		return links.Report{}, err
	}
	return links.Report{Docs: docs}, nil
}
