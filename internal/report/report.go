// Package report renders drift findings as text or JSON.
//
// # JSON schema (schemaVersion 1)
//
//	{
//	  "schemaVersion": 1,
//	  "mode": "diff",
//	  "base": "main",
//	  "drifts": [
//	    {
//	      "doc": "README.md",
//	      "section": "Usage",
//	      "line": 12,
//	      "files": ["src/a.go", "old/b.go -> src/b.go"],
//	      "severity": "high",
//	      "brokenReferences": [{"kind": "flag", "value": "--gone", "line": 14}]
//	    }
//	  ]
//	}
//
// Fields:
//
//   - schemaVersion: integer, bumped on incompatible changes
//   - mode: "diff" (base...HEAD was compared) or "history" (commit dates were compared)
//   - base: the base reference that was compared (base...HEAD); the empty string in history mode
//   - drifts: always an array, empty when nothing drifted; ordered by document, then section position
//   - doc: documentation file, slash-separated, relative to the repository root
//   - section: heading text of the drifted section
//   - line: 1-based first line of the section in doc
//   - files: always an array; the files that triggered the drift, sorted. In diff
//     mode these are the changed files (a renamed file is written as "old -> new",
//     a deleted file by its old path); in history mode the files newer than the section
//   - severity: "high" when a flag, environment variable or identifier named in the
//     section's own text existed in the section's mapped code at the merge base and
//     exists in none of it at HEAD; "low" otherwise. History mode has no base
//     version, so every drift there is "low". Severity never adds or removes a drift
//   - brokenReferences: always an array, empty unless severity is "high"; the
//     references that make it high, in document order, each with its kind
//     ("flag", "env-var" or "identifier"), value as written in the section
//     (flags with leading "--") and the 1-based line in doc
//
// New optional fields may be added without a version bump; consumers must ignore unknown fields.
//
// # Explain JSON schema (schemaVersion 1)
//
// WriteExplainJSON writes one object for one section (see testdata/explain_*.json.golden
// for complete examples). Field order is fixed, lists are never null, and no file
// content is ever included, only paths, hashes, subjects and dates.
//
//   - schemaVersion, mode, base: as above ("base" is "" in history mode)
//   - section: {key, doc, anchor, heading, line, ownRange: {start, end}}; key is
//     "<doc>#<anchor>", ownRange the section's own lines (child sections excluded)
//   - verdict: "drift" or "no-drift", exactly what check reports for the section
//   - reason: why. For "drift": "code-changed" (diff), "code-newer" (history),
//     "lock-mismatch" (history, the lockfile decides). For "no-drift":
//     "no-mapped-code", "no-mapped-file-changed", "all-changes-ignored",
//     "own-lines-changed", "acknowledged", "code-not-newer", "no-dated-code",
//     "no-blame-data"
//   - severity: "low" or "high"; present only for "drift"
//   - triggeringFiles: the files check lists for the drift (same strings); [] without drift
//   - brokenReferences: as in the check report; [] without drift
//   - links: [{file, origin}] with origin "annotation", "config" or "implicit"
//   - unresolved: [{ref, origin, line?}], references or globs that match no tracked file
//   - unmapped: true when the section has neither links nor unresolved references
//   - ignoreRules: the configured ignore globs, in order
//   - files: one entry per linked file, sorted by path:
//     path, origin, triggers (the file made the section drift before the lockfile was
//     applied), changes, commits, commitsTruncated, ignoredGlob (history mode, only when
//     the file is ignored), newestCommit (history mode; null when ignored or none counts).
//     changes (diff mode, [] when unchanged): [{status, path, oldPath?, relevance}] with
//     git's status letter and relevance {relevant, reason, glob?, oldGlob?, analyzer?}.
//     relevance.reason is "ignored-path", "whitespace-only", "api-unchanged" (dropped) or
//     "api-changed", "analyzer-failed", "rename-across-ignore", "no-analyzer",
//     "path-based" (kept); glob and oldGlob name the matching ignore globs, analyzer the
//     file extension of the symbol analyzer that was consulted.
//     commits (diff mode, only requested sections): [{hash, date, subject}] of base..HEAD
//     touching the file, newest first, at most 20; commitsTruncated says more existed.
//     newestCommit has the same shape (history mode)
//   - own: diff mode: {docChanged, hunks: [{start, lines}]}, the changed ranges touching
//     the section's own lines (lines is 0 for a pure deletion after line start)
//   - sectionDate: history mode: {hash, date} of the newest commit blamed for the own
//     lines; null in diff mode and without blame data
//   - lock: {state: "none"|"matches"|"mismatch", differing: [files], effect:
//     "none"|"suppressed"|"forced-drift"|"no-effect", driftedBeforeLock}
//
// Dates are RFC 3339 in UTC (committer time). Subjects are untrusted text. The text
// report neutralises control characters and escape sequences in every repository-provided
// value and cuts values at 200 characters.
package report

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/JtheGunner/doc-drift-checker/internal/config"
	"github.com/JtheGunner/doc-drift-checker/internal/detect"
)

// SchemaVersion is the version of the JSON output schema.
const SchemaVersion = 1

// Detection modes reported in the JSON "mode" field.
const (
	ModeDiff    = "diff"
	ModeHistory = "history"
)

type jsonReference struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
	Line  int    `json:"line"`
}

type jsonDrift struct {
	Doc              string          `json:"doc"`
	Section          string          `json:"section"`
	Line             int             `json:"line"`
	Files            []string        `json:"files"`
	Severity         string          `json:"severity"`
	BrokenReferences []jsonReference `json:"brokenReferences"`
}

type jsonReport struct {
	SchemaVersion int         `json:"schemaVersion"`
	Mode          string      `json:"mode"`
	Base          string      `json:"base"`
	Drifts        []jsonDrift `json:"drifts"`
}

// severityOf reads a zero-value severity as low.
func severityOf(d detect.Drift) string {
	if d.Severity == config.SeverityHigh {
		return string(config.SeverityHigh)
	}
	return string(config.SeverityLow)
}

// WriteJSON writes the drifts as indented JSON following the documented schema.
// base is empty in history mode.
func WriteJSON(w io.Writer, mode, base string, drifts []detect.Drift) error {
	out := jsonReport{SchemaVersion: SchemaVersion, Mode: mode, Base: base, Drifts: make([]jsonDrift, 0, len(drifts))}
	for _, d := range drifts {
		files := d.Files
		if files == nil {
			files = []string{}
		}
		broken := make([]jsonReference, 0, len(d.BrokenReferences))
		for _, r := range d.BrokenReferences {
			broken = append(broken, jsonReference{Kind: string(r.Kind), Value: r.Value, Line: r.Line})
		}
		out.Drifts = append(out.Drifts, jsonDrift{
			Doc: d.Doc, Section: d.Section, Line: d.Line, Files: files,
			Severity: severityOf(d), BrokenReferences: broken,
		})
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// WriteText writes one block per drift: the severity and section, its location,
// the triggering files and, when there are any, the broken references. Nothing
// is written when there is no drift.
func WriteText(w io.Writer, drifts []detect.Drift) error {
	for _, d := range drifts {
		if _, err := fmt.Fprintf(w, "[%s] %s\n  %s:%d\n  changed code:\n", severityOf(d), d.Section, d.Doc, d.Line); err != nil {
			return err
		}
		for _, f := range d.Files {
			if _, err := fmt.Fprintf(w, "    %s\n", f); err != nil {
				return err
			}
		}
		if len(d.BrokenReferences) == 0 {
			continue
		}
		if _, err := fmt.Fprint(w, "  broken references:\n"); err != nil {
			return err
		}
		for _, r := range d.BrokenReferences {
			if _, err := fmt.Fprintf(w, "    %s %s (%s:%d)\n", r.Kind, r.Value, d.Doc, r.Line); err != nil {
				return err
			}
		}
	}
	return nil
}
