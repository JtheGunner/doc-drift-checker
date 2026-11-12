package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/JtheGunner/doc-drift-checker/internal/config"
	"github.com/JtheGunner/doc-drift-checker/internal/docparse"
	"github.com/JtheGunner/doc-drift-checker/internal/gitx"
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

// buildLinksReport gathers the inputs (repository root, config, tracked files,
// document contents) and resolves every configured documentation file.
func buildLinksReport() (links.Report, error) {
	wd, err := os.Getwd()
	if err != nil {
		return links.Report{}, fmt.Errorf("get working directory: %w", err)
	}
	git := gitx.New(wd)
	root, err := git.Root()
	if err != nil {
		return links.Report{}, fmt.Errorf("find repository root: %w", err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		return links.Report{}, err
	}
	tracked, err := git.TrackedFiles()
	if err != nil {
		return links.Report{}, err
	}

	report := links.Report{Docs: []links.DocLinks{}}
	for _, doc := range links.MatchDocs(cfg.Docs, tracked) {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(doc)))
		if err != nil {
			return links.Report{}, fmt.Errorf("read %s: %w", doc, err)
		}
		sections := docparse.ParseSections(src)
		report.Docs = append(report.Docs, links.Resolve(doc, sections, cfg, tracked))
	}
	return report, nil
}
