package cli

import (
	"fmt"
	"path/filepath"

	"github.com/JtheGunner/doc-drift-checker/internal/detect"
	"github.com/JtheGunner/doc-drift-checker/internal/gitx"
	"github.com/JtheGunner/doc-drift-checker/internal/links"
	"github.com/JtheGunner/doc-drift-checker/internal/relevance"
)

// evaluateHistory dates every section and its mapped code from the commit
// history at HEAD and evaluates every section: a section drifts when it is
// older than its code. See the detect package comment for the limits of this
// mode. A non-empty section names the section whose commit subjects are
// gathered as well (explain).
func evaluateHistory(ignoreRevsFile, section string) (evalResult, error) {
	ignoredRevs := map[string]bool{}
	if ignoreRevsFile != "" {
		abs, err := filepath.Abs(ignoreRevsFile)
		if err != nil {
			return evalResult{}, fmt.Errorf("resolve --ignore-revs-file: %w", err)
		}
		if ignoredRevs, err = gitx.ParseIgnoreRevs(abs); err != nil {
			return evalResult{}, err
		}
		ignoreRevsFile = abs
	}
	in, err := loadRepoInputs()
	if err != nil {
		return evalResult{}, err
	}
	// Run git from the repository root so every path is root-relative.
	git := gitx.New(in.root)
	if err := git.EnsureFullHistory(); err != nil {
		return evalResult{}, err
	}
	locked, err := loadLock(in.root)
	if err != nil {
		return evalResult{}, err
	}
	headFiles, err := git.HeadFiles()
	if err != nil {
		return evalResult{}, err
	}
	// Only files committed at HEAD can be dated; staged-only files and deleted
	// files are left out of both the document list and the link targets.
	in.tracked = headFiles
	docs, err := in.resolveDocs(headFiles)
	if err != nil {
		return evalResult{}, err
	}
	target, err := resolveTarget(docs, section)
	if err != nil {
		return evalResult{}, err
	}
	dates := historyDates{
		git:            git,
		filter:         relevance.NewFilter(in.cfg.Ignore),
		ignoreRevsFile: ignoreRevsFile,
		ignoredRevs:    ignoredRevs,
		files:          map[string]detect.HistoryFile{},
	}
	sectionDates := map[detect.SectionKey]detect.CommitRef{}
	for _, doc := range docs {
		for i, section := range doc.Sections {
			if err := dates.addFiles(section); err != nil {
				return evalResult{}, err
			}
			if !dates.hasDatedFile(section) {
				continue
			}
			start, end := detect.OwnRange(doc.Sections, i)
			date, ok, err := dates.sectionDate(doc.Doc, start, end)
			if err != nil {
				return evalResult{}, err
			}
			if ok {
				sectionDates[detect.SectionKey{Doc: doc.Doc, Line: section.StartLine}] = date
			}
		}
	}
	if target.set {
		if err := dates.addSubjects(target.section); err != nil {
			return evalResult{}, err
		}
	}
	// A lockfile entry takes precedence over blame dating for its section.
	verdicts, err := lockVerdicts(git, dates.filter, docs, locked)
	if err != nil {
		return evalResult{}, err
	}
	evals := detect.Evaluate(detect.EvalInput{
		Mode: detect.ModeHistory, Docs: docs, IgnoreRules: dates.filter.Globs(),
		SectionDates: sectionDates, HistoryFiles: dates.files, Lock: verdicts,
	})
	return evalResult{evals: evals, failOn: in.cfg.Severity.FailOn, target: target.key}, nil
}

// historyDates gathers commit dates from git. files holds every linked code
// file: ignored ones with their glob, the others with their newest commit.
type historyDates struct {
	git            *gitx.Client
	filter         relevance.Filter
	ignoreRevsFile string
	ignoredRevs    map[string]bool
	files          map[string]detect.HistoryFile
}

// addFiles dates the section's linked files; a file matching an ignore glob is
// recorded as ignored and not dated.
func (d *historyDates) addFiles(section links.SectionLinks) error {
	for _, l := range section.Links {
		if _, seen := d.files[l.File]; seen {
			continue
		}
		if glob, ignored := d.filter.MatchingGlob(l.File); ignored {
			d.files[l.File] = detect.HistoryFile{IgnoredGlob: glob}
			continue
		}
		file := detect.HistoryFile{}
		commits, err := d.git.CommitTimes(l.File)
		if err != nil {
			return err
		}
		if newest, ok := gitx.LatestCommit(commits, d.ignoredRevs); ok {
			file.Newest = &detect.CommitRef{Hash: newest.Hash, Time: newest.Time}
		}
		d.files[l.File] = file
	}
	return nil
}

func (d *historyDates) hasDatedFile(section links.SectionLinks) bool {
	for _, l := range section.Links {
		if d.files[l.File].Newest != nil {
			return true
		}
	}
	return false
}

// addSubjects looks up the commit subject of the newest commit of every dated
// file of the section.
func (d *historyDates) addSubjects(section links.SectionLinks) error {
	for _, l := range section.Links {
		file := d.files[l.File]
		if file.Newest == nil || file.Newest.Subject != "" {
			continue
		}
		subject, err := d.git.CommitSubject(file.Newest.Hash)
		if err != nil {
			return err
		}
		file.Newest = &detect.CommitRef{Hash: file.Newest.Hash, Time: file.Newest.Time, Subject: subject}
		d.files[l.File] = file
	}
	return nil
}

// sectionDate is the newest commit blamed for lines start..end of doc.
func (d *historyDates) sectionDate(doc string, start, end int) (detect.CommitRef, bool, error) {
	lines, err := d.git.BlameIgnoring(doc, start, end, d.ignoreRevsFile)
	if err != nil {
		return detect.CommitRef{}, false, fmt.Errorf("blame %s lines %d-%d: %w", doc, start, end, err)
	}
	var newest detect.CommitRef
	found := false
	for _, line := range lines {
		if !found || line.Time > newest.Time {
			newest, found = detect.CommitRef{Hash: line.Commit, Time: line.Time}, true
		}
	}
	return newest, found, nil
}
