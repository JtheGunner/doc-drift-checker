package cli

import (
	"errors"

	"github.com/JtheGunner/doc-drift-checker/internal/config"
	"github.com/JtheGunner/doc-drift-checker/internal/detect"
	"github.com/JtheGunner/doc-drift-checker/internal/gitx"
	"github.com/JtheGunner/doc-drift-checker/internal/links"
)

// maxExplainCommits caps the commits listed per file in an explanation.
const maxExplainCommits = 20

// evalOptions selects what to evaluate. check and explain use the same ones.
type evalOptions struct {
	base           string
	diffMode       bool
	ignoreRevsFile string
	// section is the explain argument (see findSection). When set, commit
	// details are gathered for that section only; check leaves it empty.
	section string
}

// evalResult is the evaluation of every section and the configured severity at
// which drifts fail a check run. target is the key of the requested section.
type evalResult struct {
	evals  detect.Evaluations
	failOn config.SeverityLevel
	target string
}

// evaluate validates the mode flags and evaluates every section of every
// configured document: diff mode when diffMode is set, history mode otherwise.
// It is the only place where check and explain decide anything.
func evaluate(opts evalOptions) (evalResult, error) {
	if !opts.diffMode {
		return evaluateHistory(opts.ignoreRevsFile, opts.section)
	}
	if opts.base == "" {
		return evalResult{}, errors.New("--base needs a reference")
	}
	if opts.ignoreRevsFile != "" {
		return evalResult{}, errors.New("--ignore-revs-file only applies to history mode; remove --base or the flag")
	}
	return evaluateDiff(opts.base, opts.section)
}

// target is the section an explanation is requested for; the zero value means
// no section (check).
type target struct {
	key     string
	section links.SectionLinks
	set     bool
}

// resolveTarget resolves the explain argument against the resolved documents.
func resolveTarget(docs []links.DocLinks, arg string) (target, error) {
	if arg == "" {
		return target{}, nil
	}
	key, section, err := findSection(docs, arg)
	if err != nil {
		return target{}, err
	}
	return target{key: key, section: section, set: true}, nil
}

// commitDetails lists, for every file linked by the target section, the commits
// of base..HEAD touching it (both paths of a rename), newest first and capped.
func commitDetails(git *gitx.Client, base string, changes []gitx.Change, t target) (map[string]detect.FileCommits, error) {
	if !t.set {
		return nil, nil
	}
	details := map[string]detect.FileCommits{}
	for _, l := range t.section.Links {
		if _, done := details[l.File]; done {
			continue
		}
		paths := []string{l.File}
		for _, c := range changes {
			if c.Status != gitx.StatusRenamed {
				continue
			}
			switch l.File {
			case c.Path:
				paths = append(paths, c.OldPath)
			case c.OldPath:
				paths = append(paths, c.Path)
			}
		}
		commits, err := git.CommitsBetween(base, paths, maxExplainCommits+1)
		if err != nil {
			return nil, err
		}
		entry := detect.FileCommits{}
		if len(commits) > maxExplainCommits {
			entry.Truncated = true
			commits = commits[:maxExplainCommits]
		}
		for _, c := range commits {
			entry.Commits = append(entry.Commits, detect.CommitRef{Hash: c.Hash, Time: c.Time, Subject: c.Subject})
		}
		details[l.File] = entry
	}
	return details, nil
}
