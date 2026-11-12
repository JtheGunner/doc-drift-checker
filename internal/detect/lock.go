package detect

import (
	"github.com/JtheGunner/doc-drift-checker/internal/config"
	"github.com/JtheGunner/doc-drift-checker/internal/docparse"
	"github.com/JtheGunner/doc-drift-checker/internal/links"
)

// ApplyLock merges the lockfile verdicts into the drifts of a check run.
//
// locked has an entry for every section that is acknowledged in the lockfile.
// The value lists the mapped files whose current state differs from the locked
// state (changed hash, added, removed; sorted); an empty list means the locked
// state still matches. Sections without an entry are untouched.
//
// Diff mode (history false): a drift of a section whose locked state matches
// is suppressed; otherwise the normal result stands.
//
// History mode: a lock entry takes precedence over blame dating. The section
// drifts iff the state does not match, listing the differing files; a matching
// state is never reported, however new the code is.
//
// The result follows document and section order.
func ApplyLock(docs []links.DocLinks, drifts []Drift, locked map[SectionKey][]string, history bool) []Drift {
	if len(locked) == 0 {
		return drifts
	}
	existing := make(map[SectionKey]Drift, len(drifts))
	for _, d := range drifts {
		existing[SectionKey{Doc: d.Doc, Line: d.Line}] = d
	}
	out := []Drift{}
	for _, doc := range docs {
		for _, section := range doc.Sections {
			key := SectionKey{Doc: doc.Doc, Line: section.StartLine}
			d, drifted := existing[key]
			changed, hasEntry := locked[key]
			switch {
			case !hasEntry:
				if drifted {
					out = append(out, d)
				}
			case len(changed) == 0:
				// The maintainer confirmed exactly this code state.
			case !history:
				if drifted {
					out = append(out, d)
				}
			case drifted:
				d.Files = changed
				out = append(out, d)
			default:
				out = append(out, Drift{
					Doc: doc.Doc, Section: section.Heading, Line: section.StartLine, Files: changed,
					Severity: config.SeverityLow, BrokenReferences: []docparse.Reference{},
				})
			}
		}
	}
	return out
}
