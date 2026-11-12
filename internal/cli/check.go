package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/JtheGunner/doc-drift-checker/internal/detect"
	"github.com/JtheGunner/doc-drift-checker/internal/gitx"
	"github.com/JtheGunner/doc-drift-checker/internal/relevance"
	"github.com/JtheGunner/doc-drift-checker/internal/report"
)

const (
	formatText = "text"
	formatJSON = "json"
)

func newCheckCmd() *cobra.Command {
	var (
		base     string
		format   string
		warnOnly bool
	)
	cmd := &cobra.Command{
		Use:   "check --base <ref>",
		Short: "Report sections whose code changed while the section did not",
		Long: "Compare base...HEAD and report documentation sections whose linked code " +
			"changed while the section's own text stayed unchanged.\n\n" +
			"Exit codes: 0 no drift, 1 drift found, 2 usage or runtime error. " +
			"--warn-only forces 0 when drift is found.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			drifts, err := runCheck(base, format)
			if err != nil {
				return &exitError{code: ExitFailure, err: err}
			}
			if err := writeReport(cmd.OutOrStdout(), format, base, drifts); err != nil {
				return &exitError{code: ExitFailure, err: err}
			}
			if len(drifts) > 0 && !warnOnly {
				return &exitError{code: ExitDrift}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&base, "base", "", "base reference to compare against (required)")
	cmd.Flags().StringVar(&format, "format", formatText, "output format: text or json")
	cmd.Flags().BoolVar(&warnOnly, "warn-only", false, "exit 0 even when drift is found")
	return cmd
}

func writeReport(w io.Writer, format, base string, drifts []detect.Drift) error {
	if format == formatJSON {
		return report.WriteJSON(w, base, drifts)
	}
	return report.WriteText(w, drifts)
}

// runCheck validates the flags, gathers the git data for base...HEAD and detects drift.
func runCheck(base, format string) ([]detect.Drift, error) {
	if format != formatText && format != formatJSON {
		return nil, fmt.Errorf("invalid --format %q: use %s or %s", format, formatText, formatJSON)
	}
	if base == "" {
		return nil, errors.New("--base <ref> is required")
	}
	in, err := loadRepoInputs()
	if err != nil {
		return nil, err
	}
	changes, err := in.git.ChangedFiles(base)
	if err != nil {
		return nil, err
	}
	// Files removed from the tree (deleted, or renamed away) are no longer
	// tracked, but sections may still link to them.
	docs, err := in.resolveDocs(withRemovedPaths(in.tracked, changes))
	if err != nil {
		return nil, err
	}
	docChanges := map[string][]detect.LineRange{}
	for _, doc := range docs {
		change, ok := findChange(changes, doc.Doc)
		if !ok {
			continue
		}
		hunks, err := in.git.Hunks(base, change)
		if err != nil {
			return nil, err
		}
		docChanges[doc.Doc] = lineRanges(hunks)
	}
	relevant, err := relevantChanges(in.git, base, relevance.NewFilter(in.cfg.Ignore), changes)
	if err != nil {
		return nil, err
	}
	return detect.Detect(detect.Input{Docs: docs, Changes: detectChanges(relevant), DocChanges: docChanges}), nil
}

// relevantChanges drops the changes that cannot make documentation stale:
// ignored paths and whitespace-only modifications. Documentation edits are not
// filtered here; they are read from the unfiltered change list.
func relevantChanges(git *gitx.Client, base string, filter relevance.Filter, changes []gitx.Change) ([]gitx.Change, error) {
	relevant := make([]gitx.Change, 0, len(changes))
	for _, c := range changes {
		whitespaceOnly := false
		if filter.NeedsContentCheck(c) {
			var err error
			if whitespaceOnly, err = git.WhitespaceOnly(base, c); err != nil {
				return nil, err
			}
		}
		if filter.Relevant(c, whitespaceOnly) {
			relevant = append(relevant, c)
		}
	}
	return relevant, nil
}

// withRemovedPaths returns tracked plus the paths that existed at the base but not at HEAD.
func withRemovedPaths(tracked []string, changes []gitx.Change) []string {
	files := append([]string{}, tracked...)
	for _, c := range changes {
		switch c.Status {
		case gitx.StatusDeleted:
			files = append(files, c.Path)
		case gitx.StatusRenamed:
			files = append(files, c.OldPath)
		}
	}
	return files
}

func findChange(changes []gitx.Change, path string) (gitx.Change, bool) {
	for _, c := range changes {
		if c.Path == path {
			return c, true
		}
	}
	return gitx.Change{}, false
}

func lineRanges(hunks []gitx.Hunk) []detect.LineRange {
	ranges := make([]detect.LineRange, 0, len(hunks))
	for _, h := range hunks {
		ranges = append(ranges, detect.LineRange{Start: h.NewStart, Lines: h.NewLines})
	}
	return ranges
}

// detectChanges converts git changes; only renames keep their old path, since a
// copy leaves the source file unchanged.
func detectChanges(changes []gitx.Change) []detect.Change {
	out := make([]detect.Change, 0, len(changes))
	for _, c := range changes {
		dc := detect.Change{Path: c.Path}
		if c.Status == gitx.StatusRenamed {
			dc.OldPath = c.OldPath
		}
		out = append(out, dc)
	}
	return out
}
