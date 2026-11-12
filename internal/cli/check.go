package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/JtheGunner/doc-drift-checker/internal/config"
	"github.com/JtheGunner/doc-drift-checker/internal/detect"
	"github.com/JtheGunner/doc-drift-checker/internal/gitx"
	"github.com/JtheGunner/doc-drift-checker/internal/lock"
	"github.com/JtheGunner/doc-drift-checker/internal/relevance"
	"github.com/JtheGunner/doc-drift-checker/internal/report"
)

const (
	formatText = "text"
	formatJSON = "json"
)

func newCheckCmd() *cobra.Command {
	var (
		base           string
		format         string
		ignoreRevsFile string
		warnOnly       bool
		minSeverity    string
	)
	cmd := &cobra.Command{
		Use:   "check [--base <ref>] [--ignore-revs-file <path>]",
		Short: "Report sections whose code changed while the section did not",
		Long: "Report documentation sections whose linked code changed while the section's " +
			"own text stayed unchanged. Two modes share reporters and exit codes.\n\n" +
			"Diff mode (--base <ref>): compare base...HEAD and report sections whose linked " +
			"code changed while the section's own lines did not. Go files only count when their " +
			"exported API changed (declarations, signatures, struct fields, constants, variables); " +
			"bodies, comments, imports and unexported code are ignored, and a file that does not " +
			"parse counts as changed. Renames are never dropped, and other languages fall back to " +
			"path-based relevance. This symbol check applies to diff mode only.\n\n" +
			"History mode (no --base): needs no base branch, which suits local checks. A section " +
			"drifts when the newest commit of a linked code file is strictly newer than the " +
			"newest commit blamed for the section's own lines (git blame, child sections " +
			"excluded). Dates are committer times; equal dates are not drift. Paths matching the " +
			"ignore globs are not considered. Limits of this mode:\n" +
			"  - Squash merges: the squash commit carries the merge time for every line it touches, " +
			"so code and docs changed in one squash are equal (no drift) and the ordering of the " +
			"commits inside the squashed branch is lost.\n" +
			"  - Rewritten history (rebase, amend, filter-branch) changes committer times, so " +
			"results can change.\n" +
			"  - Formatting-only commits distort dates; list them in a file and pass " +
			"--ignore-revs-file (git's format: one full hash per line, # comments allowed).\n" +
			"  - Lines deleted from a section are invisible to blame.\n" +
			"  - Only HEAD is read; uncommitted changes are ignored, and deleted files cannot be dated.\n" +
			"  - Full history is required; shallow clones are rejected (run git fetch --unshallow).\n\n" +
			"Severity: every drift is low or high. A drift is high when a flag, environment " +
			"variable or identifier named in the section's own text existed in the section's mapped " +
			"code at the merge base and exists in none of it at HEAD (renamed or removed). Paths, " +
			"links and commands are not checked (the path mapping covers them), and a reference that " +
			"was never in the mapped code is not claimed. Limits: matching is literal on whole tokens " +
			"(a flag matches --name or the quoted bare name; identifiers need three or more characters " +
			"and are not common words); code comments count as occurrences, so a removed flag still " +
			"mentioned in a comment is not detected; mapped files above 4 MiB are unknown and never " +
			"make a drift high. History mode has no base version, so every drift there is low.\n\n" +
			"Acknowledged sections: a section with an entry in " + lock.FileName + " (see the ack " +
			"command) is decided by the lockfile. Diff mode suppresses its drift while the mapped " +
			"code equals the acknowledged state; history mode ignores dates for it and reports a " +
			"drift iff the mapped code differs from that state (run ack again after updating the " +
			"docs). Only path ignore rules apply to this comparison; whitespace and symbol " +
			"relevance cannot be evaluated against stored hashes. A malformed lockfile is an error.\n\n" +
			"Exit codes: 0 no drift (or none at or above the minimum severity), 1 drift found, " +
			"2 usage or runtime error. --min-severity low|high sets the lowest severity that exits 1 " +
			"(default: severity.fail-on from .docdrift.yaml, which defaults to low); all drifts are " +
			"printed regardless. --warn-only forces 0 when drift is found.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var failOn config.SeverityLevel
			if cmd.Flags().Changed("min-severity") {
				level, err := parseSeverity(minSeverity)
				if err != nil {
					return &exitError{code: ExitFailure, err: err}
				}
				failOn = level
			}
			opts := checkOptions{
				base:           base,
				diffMode:       cmd.Flags().Changed("base"),
				format:         format,
				ignoreRevsFile: ignoreRevsFile,
			}
			result, err := runCheck(opts)
			if err != nil {
				return &exitError{code: ExitFailure, err: err}
			}
			drifts := result.drifts
			if failOn == "" {
				failOn = result.failOn
			}
			if err := writeReport(cmd.OutOrStdout(), opts, drifts); err != nil {
				return &exitError{code: ExitFailure, err: err}
			}
			if failsAt(drifts, failOn) && !warnOnly {
				return &exitError{code: ExitDrift}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&base, "base", "", "base reference to compare against (diff mode); without it history mode runs")
	cmd.Flags().StringVar(&format, "format", formatText, "output format: text or json")
	cmd.Flags().StringVar(&ignoreRevsFile, "ignore-revs-file", "", "history mode only: file of commit hashes to skip when dating (formatting commits)")
	cmd.Flags().StringVar(&minSeverity, "min-severity", string(config.SeverityLow), "lowest severity that makes the exit code 1: low or high (default: severity.fail-on in .docdrift.yaml, else low)")
	cmd.Flags().BoolVar(&warnOnly, "warn-only", false, "exit 0 even when drift is found")
	return cmd
}

// checkOptions are the validated-later flag values of the check command.
type checkOptions struct {
	base           string
	diffMode       bool
	format         string
	ignoreRevsFile string
}

// checkResult is the outcome of a check run: the drifts and the configured
// severity at which they fail the run.
type checkResult struct {
	drifts []detect.Drift
	failOn config.SeverityLevel
}

func writeReport(w io.Writer, opts checkOptions, drifts []detect.Drift) error {
	if opts.format == formatJSON {
		if opts.diffMode {
			return report.WriteJSON(w, report.ModeDiff, opts.base, drifts)
		}
		return report.WriteJSON(w, report.ModeHistory, "", drifts)
	}
	return report.WriteText(w, drifts)
}

// runCheck validates the flags and runs diff mode (--base given) or history mode.
func runCheck(opts checkOptions) (checkResult, error) {
	if opts.format != formatText && opts.format != formatJSON {
		return checkResult{}, fmt.Errorf("invalid --format %q: use %s or %s", opts.format, formatText, formatJSON)
	}
	if !opts.diffMode {
		return runHistoryCheck(opts.ignoreRevsFile)
	}
	if opts.base == "" {
		return checkResult{}, errors.New("--base needs a reference")
	}
	if opts.ignoreRevsFile != "" {
		return checkResult{}, errors.New("--ignore-revs-file only applies to history mode; remove --base or the flag")
	}
	return runDiffCheck(opts.base)
}

// runDiffCheck gathers the git data for base...HEAD and detects drift.
func runDiffCheck(base string) (checkResult, error) {
	in, err := loadRepoInputs()
	if err != nil {
		return checkResult{}, err
	}
	locked, err := loadLock(in.root)
	if err != nil {
		return checkResult{}, err
	}
	changes, err := in.git.ChangedFiles(base)
	if err != nil {
		return checkResult{}, err
	}
	// Files removed from the tree (deleted, or renamed away) are no longer
	// tracked, but sections may still link to them.
	docs, sections, err := in.resolveDocsWithSections(withRemovedPaths(in.tracked, changes))
	if err != nil {
		return checkResult{}, err
	}
	docChanges := map[string][]detect.LineRange{}
	for _, doc := range docs {
		change, ok := findChange(changes, doc.Doc)
		if !ok {
			continue
		}
		hunks, err := in.git.Hunks(base, change)
		if err != nil {
			return checkResult{}, err
		}
		docChanges[doc.Doc] = lineRanges(hunks)
	}
	relevant, err := relevantChanges(in.git, base, relevance.NewFilter(in.cfg.Ignore), relevance.NewAnalyzerSet(relevance.DefaultAnalyzers()...), changes)
	if err != nil {
		return checkResult{}, err
	}
	drifts := detect.Detect(detect.Input{Docs: docs, Changes: detectChanges(relevant), DocChanges: docChanges})
	// A drift of a section whose code still equals its acknowledged state is
	// dropped; this runs before severity so suppressed sections are not read.
	verdicts, err := lockVerdicts(gitx.New(in.root), relevance.NewFilter(in.cfg.Ignore), docs, locked)
	if err != nil {
		return checkResult{}, err
	}
	drifts = detect.ApplyLock(docs, drifts, verdicts, false)
	// Severity reads the mapped files of every drifted section, so it sees the
	// unfiltered change list: a file the relevance filter dropped still holds
	// (or no longer holds) the referenced names.
	drifts, err = classifyDrifts(in.git, base, docs, sections, changes, drifts)
	if err != nil {
		return checkResult{}, err
	}
	return checkResult{drifts: drifts, failOn: in.cfg.Severity.FailOn}, nil
}

// relevantChanges drops the changes that cannot make documentation stale:
// ignored paths, whitespace-only modifications and, for languages with a
// SymbolAnalyzer, changes that leave the public interface untouched. Other
// languages keep every change that survives the path rules (path-based
// fallback). Documentation edits are not filtered here; they are read from the
// unfiltered change list.
func relevantChanges(git *gitx.Client, base string, filter relevance.Filter, analyzers relevance.AnalyzerSet, changes []gitx.Change) ([]gitx.Change, error) {
	relevant := make([]gitx.Change, 0, len(changes))
	mergeBase := ""
	for _, c := range changes {
		if filter.PathIgnored(c) {
			continue
		}
		whitespaceOnly := false
		if filter.NeedsContentCheck(c) {
			var err error
			if whitespaceOnly, err = git.WhitespaceOnly(base, c); err != nil {
				return nil, err
			}
		}
		if !filter.Relevant(c, whitespaceOnly) {
			continue
		}
		if analyzers.Supports(c) {
			if mergeBase == "" {
				var err error
				if mergeBase, err = git.MergeBase(base); err != nil {
					return nil, err
				}
			}
			changed, err := apiChanged(git, mergeBase, analyzers, c)
			if err != nil {
				return nil, err
			}
			if !changed {
				continue
			}
		}
		relevant = append(relevant, c)
	}
	return relevant, nil
}

// apiChanged reads both versions of a file and asks the analyzer whether its
// public interface changed. The side that does not exist (added or deleted file)
// is passed as nil.
func apiChanged(git *gitx.Client, mergeBase string, analyzers relevance.AnalyzerSet, c gitx.Change) (bool, error) {
	var oldSrc, newSrc []byte
	var err error
	if c.Status != gitx.StatusAdded {
		if oldSrc, err = readOptional(git, mergeBase, c.Path); err != nil {
			return false, err
		}
	}
	if c.Status != gitx.StatusDeleted {
		if newSrc, err = readOptional(git, "HEAD", c.Path); err != nil {
			return false, err
		}
	}
	return analyzers.APIChanged(c, oldSrc, newSrc)
}

// readOptional reads a file at a revision; a missing file yields nil content.
func readOptional(git *gitx.Client, rev, path string) ([]byte, error) {
	src, err := git.FileAt(rev, path)
	if errors.Is(err, gitx.ErrNotExist) {
		return nil, nil
	}
	return src, err
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
