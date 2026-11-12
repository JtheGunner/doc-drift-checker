package relevance

import (
	"path"
	"strings"

	"github.com/JtheGunner/doc-drift-checker/internal/gitx"
)

// Reason names why a change was kept or dropped by the relevance pipeline.
type Reason string

// The relevance reasons. The first three drop a change, the others keep it.
const (
	// ReasonIgnoredPath: every path of the change matches an ignore glob.
	ReasonIgnoredPath Reason = "ignored-path"
	// ReasonWhitespaceOnly: a modification whose diff is whitespace-only.
	ReasonWhitespaceOnly Reason = "whitespace-only"
	// ReasonAPIUnchanged: a SymbolAnalyzer found the public interface unchanged.
	ReasonAPIUnchanged Reason = "api-unchanged"
	// ReasonAPIChanged: a SymbolAnalyzer found the public interface changed.
	ReasonAPIChanged Reason = "api-changed"
	// ReasonAnalyzerFailed: a SymbolAnalyzer failed, so the change counts (fail closed).
	ReasonAnalyzerFailed Reason = "analyzer-failed"
	// ReasonRenameAcrossIgnore: a rename or copy between an ignored and a
	// non-ignored path is never dropped.
	ReasonRenameAcrossIgnore Reason = "rename-across-ignore"
	// ReasonNoAnalyzer: no analyzer supports the language, so the path-based
	// fallback keeps the change.
	ReasonNoAnalyzer Reason = "no-analyzer"
	// ReasonPathBased: kept by the path rules alone (renames, copies and type
	// changes are never dropped as irrelevant content).
	ReasonPathBased Reason = "path-based"
)

// Decision is the outcome of the relevance pipeline for one change.
type Decision struct {
	Relevant bool
	Reason   Reason
	// Glob is the ignore glob matching the change's path, OldGlob the one
	// matching the old path of a rename or copy; empty when none matches.
	Glob    string
	OldGlob string
	// Analyzer is the file extension (".go") of the SymbolAnalyzer that was
	// consulted; empty when none was.
	Analyzer string
}

// Probe supplies the git facts a decision may need. Each function runs at most
// once and only when the decision depends on it.
type Probe struct {
	// WhitespaceOnly reports whether the change's diff is whitespace-only.
	WhitespaceOnly func() (bool, error)
	// Sources returns the content before and after the change (nil for a side
	// that does not exist).
	Sources func() (oldSrc, newSrc []byte, err error)
}

// Decide runs the whole relevance pipeline for one change: ignored paths,
// whitespace-only modifications, rename boundaries and the symbol analyzers.
// It is the single place that decides relevance; Relevant, PathIgnored and
// AnalyzerSet are its building blocks.
func (f Filter) Decide(analyzers AnalyzerSet, c gitx.Change, probe Probe) (Decision, error) {
	glob, _ := f.MatchingGlob(c.Path)
	oldGlob := ""
	if c.OldPath != "" {
		oldGlob, _ = f.MatchingGlob(c.OldPath)
	}
	if f.PathIgnored(c) {
		return Decision{Reason: ReasonIgnoredPath, Glob: glob, OldGlob: oldGlob}, nil
	}
	whitespaceOnly := false
	if f.NeedsContentCheck(c) {
		var err error
		if whitespaceOnly, err = probe.WhitespaceOnly(); err != nil {
			return Decision{}, err
		}
	}
	if !f.Relevant(c, whitespaceOnly) {
		return Decision{Reason: ReasonWhitespaceOnly}, nil
	}
	kept := func(reason Reason) Decision {
		return Decision{Relevant: true, Reason: reason, Glob: glob, OldGlob: oldGlob}
	}
	if c.Status == gitx.StatusRenamed || c.Status == gitx.StatusCopied {
		if (glob != "") != (oldGlob != "") {
			return kept(ReasonRenameAcrossIgnore), nil
		}
		return kept(ReasonPathBased), nil
	}
	if !analyzers.Supports(c) {
		if c.Status == gitx.StatusModified || c.Status == gitx.StatusAdded || c.Status == gitx.StatusDeleted {
			return kept(ReasonNoAnalyzer), nil
		}
		return kept(ReasonPathBased), nil
	}
	oldSrc, newSrc, err := probe.Sources()
	if err != nil {
		return Decision{}, err
	}
	ext := strings.ToLower(path.Ext(c.Path))
	changed, failed := analyzers.analyze(c, oldSrc, newSrc)
	switch {
	case failed:
		return Decision{Relevant: true, Reason: ReasonAnalyzerFailed, Analyzer: ext}, nil
	case changed:
		return Decision{Relevant: true, Reason: ReasonAPIChanged, Analyzer: ext}, nil
	}
	return Decision{Reason: ReasonAPIUnchanged, Analyzer: ext}, nil
}
