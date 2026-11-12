// Package detect finds documentation sections that drifted: their mapped code
// changed while the section's own text did not.
//
// Detection is pure. It takes already-gathered data (resolved links, the change
// list and the changed line ranges per document) and never touches git or the
// filesystem.
//
// A section drifted when at least one of its linked files is in the change set
// and no line of the section's own range changed. The own range excludes child
// sections. A pure deletion in the document counts as touching a section when
// it happens after one of the section's own lines.
package detect

import (
	"sort"

	"github.com/JtheGunner/doc-drift-checker/internal/links"
)

// Change is one changed code file. OldPath is set only for renames; a deleted
// file carries its old path in Path.
type Change struct {
	Path    string
	OldPath string
}

// Label is how the change is shown: the path, or "old -> new" for a rename.
func (c Change) Label() string {
	if c.OldPath != "" {
		return c.OldPath + " -> " + c.Path
	}
	return c.Path
}

// LineRange is a changed region on the new side of a document. Lines is zero for
// a pure deletion; Start is then the line after which the text was removed.
type LineRange struct {
	Start, Lines int
}

// Input is everything detection needs.
type Input struct {
	// Docs are the resolved links per documentation file.
	Docs []links.DocLinks
	// Changes is the set of files changed between base and HEAD.
	Changes []Change
	// DocChanges maps a documentation file to its changed line ranges.
	// A file without an entry is unchanged.
	DocChanges map[string][]LineRange
}

// Drift is one section whose code changed without the section changing.
type Drift struct {
	Doc     string
	Section string
	// Line is the first line of the section.
	Line int
	// Files are the changed files that triggered the drift, sorted by label.
	Files []string
}

// Detect returns the drifted sections in document and section order.
func Detect(in Input) []Drift {
	drifts := []Drift{}
	for _, doc := range in.Docs {
		for i, section := range doc.Sections {
			files := triggeringFiles(section, in.Changes)
			if len(files) == 0 {
				continue
			}
			if touched(ownRange(doc.Sections, i), in.DocChanges[doc.Doc]) {
				continue
			}
			drifts = append(drifts, Drift{Doc: doc.Doc, Section: section.Heading, Line: section.StartLine, Files: files})
		}
	}
	return drifts
}

func triggeringFiles(section links.SectionLinks, changes []Change) []string {
	linked := make(map[string]bool, len(section.Links))
	for _, l := range section.Links {
		linked[l.File] = true
	}
	files := []string{}
	for _, c := range changes {
		if linked[c.Path] || (c.OldPath != "" && linked[c.OldPath]) {
			files = append(files, c.Label())
		}
	}
	sort.Strings(files)
	return files
}

type span struct{ start, end int }

// ownRange is the section's range cut before its first child section.
func ownRange(sections []links.SectionLinks, i int) span {
	s := sections[i]
	for _, next := range sections[i+1:] {
		if next.StartLine > s.EndLine {
			break
		}
		if next.Level > s.Level {
			return span{s.StartLine, next.StartLine - 1}
		}
	}
	return span{s.StartLine, s.EndLine}
}

func touched(own span, ranges []LineRange) bool {
	for _, r := range ranges {
		if r.Lines == 0 {
			if r.Start >= own.start && r.Start <= own.end {
				return true
			}
			continue
		}
		if r.Start <= own.end && r.Start+r.Lines-1 >= own.start {
			return true
		}
	}
	return false
}
