package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/JtheGunner/doc-drift-checker/internal/lock"
	"github.com/JtheGunner/doc-drift-checker/internal/report"
)

func newExplainCmd() *cobra.Command {
	var (
		base           string
		format         string
		ignoreRevsFile string
	)
	cmd := &cobra.Command{
		Use:   "explain <section> [--base <ref>] [--ignore-revs-file <path>] [--format text|json]",
		Short: "Explain why a section is reported by check, or why it is not",
		Long: "Explain the result check has for one documentation section, drift or not. The " +
			"explanation is built from the very same evaluation as check: the resolved links with " +
			"their origin (annotation, config, implicit), the references that match no file, every " +
			"mapped file with its status and the relevance rule that kept or dropped it (the exact " +
			"ignore glob, whitespace-only, unchanged Go API, or the fallback), the commits of " +
			"base..HEAD touching each file, whether the section's own lines changed (diff mode) or " +
			"how its date compares with the code (history mode), the lockfile state and its effect, " +
			"and the verdict with its reason, severity and broken references.\n\n" +
			"<section> is the section key <doc path>#<anchor> (e.g. README.md#installation), or just " +
			"the anchor (with or without #) when it is unique across all configured docs; the same " +
			"resolution as ack. Without --base the history mode of check runs, with --base <ref> the " +
			"diff mode; --ignore-revs-file is history mode only. Only paths, hashes, subjects and " +
			"dates are printed, never file contents; control characters in them are neutralised in " +
			"the text output.\n\n" +
			"Exit codes: 0 the explanation was written (whether or not the section drifts), 2 usage or " +
			"runtime error (unknown or ambiguous section, bad flags, no git repository, " +
			"a malformed " + lock.FileName + ").",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := explainOptions{
				section:        args[0],
				base:           base,
				diffMode:       cmd.Flags().Changed("base"),
				format:         format,
				ignoreRevsFile: ignoreRevsFile,
			}
			if err := runExplain(cmd.OutOrStdout(), opts); err != nil {
				return &exitError{code: ExitFailure, err: err}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&base, "base", "", "base reference to compare against (diff mode); without it history mode runs")
	cmd.Flags().StringVar(&format, "format", formatText, "output format: text or json")
	cmd.Flags().StringVar(&ignoreRevsFile, "ignore-revs-file", "", "history mode only: file of commit hashes to skip when dating (formatting commits)")
	return cmd
}

// explainOptions are the flag values and the argument of the explain command.
type explainOptions struct {
	section        string
	base           string
	diffMode       bool
	format         string
	ignoreRevsFile string
}

// runExplain evaluates every section exactly as check does and writes the
// explanation of the requested one.
func runExplain(w io.Writer, opts explainOptions) error {
	if opts.format != formatText && opts.format != formatJSON {
		return fmt.Errorf("invalid --format %q: use %s or %s", opts.format, formatText, formatJSON)
	}
	result, err := evaluate(evalOptions{
		base: opts.base, diffMode: opts.diffMode, ignoreRevsFile: opts.ignoreRevsFile, section: opts.section,
	})
	if err != nil {
		return err
	}
	ev, ok := result.evals.Find(result.target)
	if !ok {
		return fmt.Errorf("section %s was not evaluated", result.target)
	}
	if opts.format == formatJSON {
		return report.WriteExplainJSON(w, ev)
	}
	return report.WriteExplainText(w, ev)
}
