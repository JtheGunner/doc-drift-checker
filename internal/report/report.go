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
