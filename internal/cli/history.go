package cli

import (
	"fmt"
	"path/filepath"

	"github.com/JtheGunner/doc-drift-checker/internal/detect"
	"github.com/JtheGunner/doc-drift-checker/internal/gitx"
	"github.com/JtheGunner/doc-drift-checker/internal/links"
	"github.com/JtheGunner/doc-drift-checker/internal/relevance"
)

// runHistoryCheck dates every section and its mapped code from the commit
// history at HEAD and reports sections older than their code. See the detect
// package comment for the limits of this mode.
func runHistoryCheck(ignoreRevsFile string) ([]detect.Drift, error) {
	ignoredRevs := map[string]bool{}
	if ignoreRevsFile != "" {
		abs, err := filepath.Abs(ignoreRevsFile)
		if err != nil {
			return nil, fmt.Errorf("resolve --ignore-revs-file: %w", err)
		}
		if ignoredRevs, err = gitx.ParseIgnoreRevs(abs); err != nil {
			return nil, err
		}
		ignoreRevsFile = abs
	}
	in, err := loadRepoInputs()
	if err != nil {
		return nil, err
	}
	// Run git from the repository root so every path is root-relative.
	git := gitx.New(in.root)
	if err := git.EnsureFullHistory(); err != nil {
		return nil, err
	}
	headFiles, err := git.HeadFiles()
	if err != nil {
		return nil, err
	}
	// Only files committed at HEAD can be dated; staged-only files and deleted
	// files are left out of both the document list and the link targets.
	in.tracked = headFiles
	docs, err := in.resolveDocs(headFiles)
	if err != nil {
		return nil, err
	}
	dates := historyDates{
		git:            git,
		filter:         relevance.NewFilter(in.cfg.Ignore),
		ignoreRevsFile: ignoreRevsFile,
		ignoredRevs:    ignoredRevs,
		files:          map[string]int64{},
		seen:           map[string]bool{},
	}
	sectionDates := map[detect.SectionKey]int64{}
	for _, doc := range docs {
		for i, section := range doc.Sections {
			if err := dates.addFiles(section); err != nil {
				return nil, err
			}
			if !dates.hasDatedFile(section) {
				continue
			}
			start, end := detect.OwnRange(doc.Sections, i)
			date, ok, err := dates.sectionDate(doc.Doc, start, end)
			if err != nil {
				return nil, err
			}
			if ok {
				sectionDates[detect.SectionKey{Doc: doc.Doc, Line: section.StartLine}] = date
			}
		}
	}
	return detect.History(detect.HistoryInput{Docs: docs, SectionDates: sectionDates, FileDates: dates.files}), nil
}

// historyDates gathers commit dates from git. files holds only the dated,
// non-ignored code files.
type historyDates struct {
	git            *gitx.Client
	filter         relevance.Filter
	ignoreRevsFile string
	ignoredRevs    map[string]bool
	files          map[string]int64
	seen           map[string]bool
}

// addFiles dates the section's linked files that are not ignored by path.
func (d *historyDates) addFiles(section links.SectionLinks) error {
	for _, l := range section.Links {
		if d.seen[l.File] || d.filter.Ignored(l.File) {
			continue
		}
		d.seen[l.File] = true
		commits, err := d.git.CommitTimes(l.File)
		if err != nil {
			return err
		}
		if date, ok := gitx.LatestTime(commits, d.ignoredRevs); ok {
			d.files[l.File] = date
		}
	}
	return nil
}

func (d *historyDates) hasDatedFile(section links.SectionLinks) bool {
	for _, l := range section.Links {
		if _, ok := d.files[l.File]; ok {
			return true
		}
	}
	return false
}

// sectionDate is the newest committer time blamed for lines start..end of doc.
func (d *historyDates) sectionDate(doc string, start, end int) (int64, bool, error) {
	lines, err := d.git.BlameIgnoring(doc, start, end, d.ignoreRevsFile)
	if err != nil {
		return 0, false, fmt.Errorf("blame %s lines %d-%d: %w", doc, start, end, err)
	}
	var newest int64
	found := false
	for _, line := range lines {
		if !found || line.Time > newest {
			newest, found = line.Time, true
		}
	}
	return newest, found, nil
}
