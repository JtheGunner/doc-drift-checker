package detect

import (
	"sort"

	"github.com/JtheGunner/doc-drift-checker/internal/links"
)

// SectionKey identifies a section by its document and first line.
type SectionKey struct {
	Doc  string
	Line int
}

// HistoryInput is everything history-mode detection needs. Dates are committer
// times in unix seconds.
type HistoryInput struct {
	// Docs are the resolved links per documentation file.
	Docs []links.DocLinks
	// SectionDates is the newest commit time of each section's own lines.
	// A section without an entry has no blame data and is never reported.
	SectionDates map[SectionKey]int64
	// FileDates is the newest relevant commit time per code file. A file
	// without an entry (ignored path, deleted, no commit) is not considered.
	FileDates map[string]int64
}

// History returns the sections that have at least one linked file with a
// date strictly newer than the section's own date, in document and section order.
// Equal dates are not drift.
func History(in HistoryInput) []Drift {
	drifts := []Drift{}
	for _, doc := range in.Docs {
		for _, section := range doc.Sections {
			sectionDate, ok := in.SectionDates[SectionKey{Doc: doc.Doc, Line: section.StartLine}]
			if !ok {
				continue
			}
			files := newerFiles(section, in.FileDates, sectionDate)
			if len(files) == 0 {
				continue
			}
			drifts = append(drifts, Drift{Doc: doc.Doc, Section: section.Heading, Line: section.StartLine, Files: files})
		}
	}
	return drifts
}

func newerFiles(section links.SectionLinks, fileDates map[string]int64, sectionDate int64) []string {
	seen := map[string]bool{}
	files := []string{}
	for _, l := range section.Links {
		date, ok := fileDates[l.File]
		if !ok || date <= sectionDate || seen[l.File] {
			continue
		}
		seen[l.File] = true
		files = append(files, l.File)
	}
	sort.Strings(files)
	return files
}
