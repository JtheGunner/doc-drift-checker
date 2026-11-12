// Package detect finds documentation sections that drifted: their mapped code
// changed while the section's own text did not.
//
// Two modes share the Drift type. Diff mode (Detect) compares base...HEAD.
// History mode (History) compares dates: see "History mode" below.
//
// Detection is pure. It takes already-gathered data (resolved links, the change
// list and the changed line ranges per document) and never touches git or the
// filesystem.
//
// A section drifted when at least one of its linked files is in the change set
// and no line of the section's own range changed. The own range excludes child
// sections. A pure deletion in the document counts as touching a section when
// it happens after one of the section's own lines.
//
// # History mode
//
// History mode needs no base reference. A section drifted when at least one
// mapped code file's date is strictly newer than the section's date. Both are
// committer times: the section date is the newest commit blamed for the
// section's own lines in the document at HEAD (line level, child sections
// excluded), the file date is the newest commit in the file's history. Commits
// listed in an ignore-revs file are skipped on both sides.
//
// Known limits, which cannot be fixed from commit data alone:
//   - Squash merges: the squash commit carries the merge time for every line it
//     touches. Code and documentation changed in one squash get equal dates and
//     are not reported, and the ordering of the commits inside the squashed
//     branch is lost.
//   - Rewritten history (rebase, amend, filter-branch) changes committer times,
//     so the result can change after a rewrite.
//   - Formatting-only commits move dates. Exclude them with --ignore-revs-file.
//   - Lines deleted from a section are invisible to blame: deleting text does
//     not make the section newer.
//   - Only HEAD is read; uncommitted changes in the working tree are ignored.
//     Deleted files cannot be dated and are not considered.
//   - Blame needs full history: shallow clones are rejected by the caller.
package detect

import (
	"sort"

	"github.com/JtheGunner/doc-drift-checker/internal/config"
	"github.com/JtheGunner/doc-drift-checker/internal/docparse"
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
	// Severity is high when a flag, env var or identifier named in the section
	// no longer exists in its mapped code, see Classify. Detect and History
	// report low; Classify refines diff-mode drifts.
	Severity config.SeverityLevel
	// BrokenReferences are the references that make the drift high; never nil.
	BrokenReferences []docparse.Reference
}

// newDrift builds a low-severity drift.
func newDrift(doc string, s links.SectionLinks, files []string) Drift {
	return Drift{Doc: doc, Section: s.Heading, Line: s.StartLine, Files: files, Severity: config.SeverityLow, BrokenReferences: []docparse.Reference{}}
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
			drifts = append(drifts, newDrift(doc.Doc, section, files))
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

// OwnRange returns the first and last line (inclusive) of sections[i]'s own
// text: its range cut before its first child section.
func OwnRange(sections []links.SectionLinks, i int) (start, end int) {
	s := sections[i]
	for _, next := range sections[i+1:] {
		if next.StartLine > s.EndLine {
			break
		}
		if next.Level > s.Level {
			return s.StartLine, next.StartLine - 1
		}
	}
	return s.StartLine, s.EndLine
}

func ownRange(sections []links.SectionLinks, i int) span {
	start, end := OwnRange(sections, i)
	return span{start, end}
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
