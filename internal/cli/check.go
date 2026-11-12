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

// runCheck validates the flags, evaluates every section and keeps the ones
// that ended as drift. The evaluation is shared with the explain command.
func runCheck(opts checkOptions) (checkResult, error) {
	if opts.format != formatText && opts.format != formatJSON {
		return checkResult{}, fmt.Errorf("invalid --format %q: use %s or %s", opts.format, formatText, formatJSON)
	}
	result, err := evaluate(evalOptions{base: opts.base, diffMode: opts.diffMode, ignoreRevsFile: opts.ignoreRevsFile})
	if err != nil {
		return checkResult{}, err
	}
	return checkResult{drifts: result.evals.Drifts(), failOn: result.failOn}, nil
}

// evaluateDiff gathers the git data for base...HEAD and evaluates every
// section. A non-empty section names the section whose commit details are
// gathered as well (explain).
func evaluateDiff(base, section string) (evalResult, error) {
	in, err := loadRepoInputs()
	if err != nil {
		return evalResult{}, err
	}
	locked, err := loadLock(in.root)
	if err != nil {
		return evalResult{}, err
	}
	changes, err := in.git.ChangedFiles(base)
	if err != nil {
		return evalResult{}, err
	}
	// Files removed from the tree (deleted, or renamed away) are no longer
	// tracked, but sections may still link to them.
	docs, sections, err := in.resolveDocsWithSections(withRemovedPaths(in.tracked, changes))
	if err != nil {
		return evalResult{}, err
	}
	target, err := resolveTarget(docs, section)
	if err != nil {
		return evalResult{}, err
	}
	docChanges := map[string][]detect.LineRange{}
	for _, doc := range docs {
		change, ok := findChange(changes, doc.Doc)
		if !ok {
			continue
		}
		hunks, err := in.git.Hunks(base, change)
		if err != nil {
			return evalResult{}, err
		}
		docChanges[doc.Doc] = lineRanges(hunks)
	}
	filter := relevance.NewFilter(in.cfg.Ignore)
	fileChanges, err := decideChanges(in.git, base, filter, relevance.NewAnalyzerSet(relevance.DefaultAnalyzers()...), changes)
	if err != nil {
		return evalResult{}, err
	}
	// The lockfile verdicts are compared before severity so that suppressed
	// sections are never read.
	verdicts, err := lockVerdicts(gitx.New(in.root), filter, docs, locked)
	if err != nil {
		return evalResult{}, err
	}
	commits, err := commitDetails(in.git, base, changes, target)
	if err != nil {
		return evalResult{}, err
	}
	evals := detect.Evaluate(detect.EvalInput{
		Mode: detect.ModeDiff, Base: base, Docs: docs, IgnoreRules: filter.Globs(),
		Changes: fileChanges, DocChanges: docChanges, Lock: verdicts, Commits: commits,
	})
	// Severity reads the mapped files of every drifted section, so it sees the
	// unfiltered change list: a file the relevance filter dropped still holds
	// (or no longer holds) the referenced names.
	evals, err = classifyEvaluations(in.git, base, docs, sections, changes, evals)
	if err != nil {
		return evalResult{}, err
	}
	return evalResult{evals: evals, failOn: in.cfg.Severity.FailOn, target: target.key}, nil
}

// decideChanges runs the relevance pipeline for every change: ignored paths,
// whitespace-only modifications and, for languages with a SymbolAnalyzer,
// changes that leave the public interface untouched. Other languages keep every
// change that survives the path rules (path-based fallback). The decisions are
// recorded for all changes; Evaluate keeps the relevant ones. Documentation
// edits are not filtered here; they are read from the unfiltered change list.
func decideChanges(git *gitx.Client, base string, filter relevance.Filter, analyzers relevance.AnalyzerSet, changes []gitx.Change) ([]detect.FileChange, error) {
	decided := make([]detect.FileChange, 0, len(changes))
	mergeBase := ""
	for _, c := range changes {
		probe := relevance.Probe{
			WhitespaceOnly: func() (bool, error) { return git.WhitespaceOnly(base, c) },
			Sources: func() ([]byte, []byte, error) {
				if mergeBase == "" {
					var err error
					if mergeBase, err = git.MergeBase(base); err != nil {
						return nil, nil, err
					}
				}
				return sourcesOf(git, mergeBase, c)
			},
		}
		decision, err := filter.Decide(analyzers, c, probe)
		if err != nil {
			return nil, err
		}
		decided = append(decided, detect.FileChange{Status: string(c.Status), Path: c.Path, OldPath: c.OldPath, Decision: decision})
	}
	return decided, nil
}

// sourcesOf reads both versions of a file. The side that does not exist (added
// or deleted file) is nil.
func sourcesOf(git *gitx.Client, mergeBase string, c gitx.Change) (oldSrc, newSrc []byte, err error) {
	if c.Status != gitx.StatusAdded {
		if oldSrc, err = readOptional(git, mergeBase, c.Path); err != nil {
			return nil, nil, err
		}
	}
	if c.Status != gitx.StatusDeleted {
		if newSrc, err = readOptional(git, "HEAD", c.Path); err != nil {
			return nil, nil, err
		}
	}
	return oldSrc, newSrc, nil
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
