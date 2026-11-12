package relevance

import (
	"path"
	"strings"

	"github.com/JtheGunner/doc-drift-checker/internal/gitx"
)

// SymbolAnalyzer decides whether the public interface of a source file changed,
// so edits that cannot affect documentation (bodies, comments, private code)
// are not reported. It is the extension point for languages: implement it and
// add the implementation to DefaultAnalyzers, nothing else is needed.
//
//	type pyAnalyzer struct{}
//
//	func (pyAnalyzer) Extensions() []string { return []string{".py"} }
//
//	func (pyAnalyzer) APIChanged(oldSrc, newSrc []byte) (bool, error) {
//		// compare the public surface of both versions
//	}
//
// Contract:
//   - Extensions lists lower-case file extensions including the dot (".go").
//   - A nil or empty source means the file does not exist on that side (added
//     or deleted file).
//   - Be conservative: when the sources cannot be understood, return true. A
//     false positive costs a review, a false negative hides drift.
//   - Implementations are pure: no filesystem or git access.
//
// Languages without an analyzer fall back to path-based relevance: any change
// that survives the path and whitespace filters counts.
type SymbolAnalyzer interface {
	Extensions() []string
	APIChanged(oldSrc, newSrc []byte) (bool, error)
}

// DefaultAnalyzers returns every registered analyzer. This is the single place
// to register a new language.
func DefaultAnalyzers() []SymbolAnalyzer {
	return []SymbolAnalyzer{
		goAnalyzer{},
	}
}

// AnalyzerSet looks analyzers up by file extension.
type AnalyzerSet struct {
	byExt map[string]SymbolAnalyzer
}

// NewAnalyzerSet indexes the analyzers by their extensions. When two analyzers
// claim the same extension, the later one wins.
func NewAnalyzerSet(analyzers ...SymbolAnalyzer) AnalyzerSet {
	byExt := map[string]SymbolAnalyzer{}
	for _, a := range analyzers {
		for _, ext := range a.Extensions() {
			byExt[strings.ToLower(ext)] = a
		}
	}
	return AnalyzerSet{byExt: byExt}
}

// For returns the analyzer responsible for the file path, or nil.
func (s AnalyzerSet) For(filePath string) SymbolAnalyzer {
	return s.byExt[strings.ToLower(path.Ext(filePath))]
}

// Supports reports whether the symbol analysis applies to the change: the
// extension has an analyzer and the status is a modification, addition or
// deletion. Renames and copies are never dropped, because the path itself may
// be referenced in documentation.
func (s AnalyzerSet) Supports(c gitx.Change) bool {
	switch c.Status {
	case gitx.StatusModified, gitx.StatusAdded, gitx.StatusDeleted:
		return s.For(c.Path) != nil
	default:
		return false
	}
}

// APIChanged reports whether the change touches the public interface. Changes
// without an analyzer are reported as changed (path-based fallback), and so is
// an analyzer failure: a broken analysis must not hide drift.
func (s AnalyzerSet) APIChanged(c gitx.Change, oldSrc, newSrc []byte) (bool, error) {
	if !s.Supports(c) {
		return true, nil
	}
	changed, err := s.For(c.Path).APIChanged(oldSrc, newSrc)
	if err != nil {
		return true, nil
	}
	return changed, nil
}
