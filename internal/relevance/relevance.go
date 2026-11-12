// Package relevance decides whether a changed file can make documentation
// stale. It is pure: callers supply the git facts (see gitx.WhitespaceOnly).
//
// A change is irrelevant when
//   - every path involved (the new path, and the old path of a rename or copy)
//     matches an ignore glob, or
//   - it is a modification (status M) whose diff is whitespace-only.
//
// Additions, deletions, renames and copies are never dropped as whitespace-only:
// a rename changes a path the documentation may mention. A rename between an
// ignored and a non-ignored path stays relevant.
package relevance

import (
	"github.com/JtheGunner/doc-drift-checker/internal/gitx"
	"github.com/JtheGunner/doc-drift-checker/internal/glob"
)

// Filter decides relevance from path globs.
type Filter struct {
	globs []string
}

// NewFilter builds a Filter from ignore globs (config.Config.Ignore).
func NewFilter(ignore []string) Filter {
	return Filter{globs: append([]string(nil), ignore...)}
}

// Ignored reports whether the slash-separated path matches an ignore glob.
func (f Filter) Ignored(path string) bool {
	for _, g := range f.globs {
		if glob.Match(g, path) {
			return true
		}
	}
	return false
}

// PathIgnored reports whether every path of the change is ignored.
func (f Filter) PathIgnored(c gitx.Change) bool {
	if !f.Ignored(c.Path) {
		return false
	}
	return c.OldPath == "" || f.Ignored(c.OldPath)
}

// NeedsContentCheck reports whether Relevant depends on the whitespaceOnly
// argument, so callers can skip the git call otherwise.
func (f Filter) NeedsContentCheck(c gitx.Change) bool {
	return c.Status == gitx.StatusModified && !f.PathIgnored(c)
}

// Relevant reports whether the change may make documentation stale.
// whitespaceOnly states whether the content diff is whitespace-only; it only
// matters for modifications.
func (f Filter) Relevant(c gitx.Change, whitespaceOnly bool) bool {
	if f.PathIgnored(c) {
		return false
	}
	return !(c.Status == gitx.StatusModified && whitespaceOnly)
}
