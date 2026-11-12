// Package report renders drift findings as text or JSON.
//
// # JSON schema (schemaVersion 1)
//
//	{
//	  "schemaVersion": 1,
//	  "base": "main",
//	  "drifts": [
//	    {
//	      "doc": "README.md",
//	      "section": "Usage",
//	      "line": 12,
//	      "files": ["src/a.go", "old/b.go -> src/b.go"]
//	    }
//	  ]
//	}
//
// Fields:
//
//   - schemaVersion: integer, bumped on incompatible changes
//   - base: the base reference that was compared (base...HEAD)
//   - drifts: always an array, empty when nothing drifted; ordered by document, then section position
//   - doc: documentation file, slash-separated, relative to the repository root
//   - section: heading text of the drifted section
//   - line: 1-based first line of the section in doc
//   - files: always an array; the changed files that triggered the drift, sorted;
//     a renamed file is written as "old -> new", a deleted file by its old path
//
// New optional fields may be added without a version bump; consumers must ignore unknown fields.
package report

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/JtheGunner/doc-drift-checker/internal/detect"
)

// SchemaVersion is the version of the JSON output schema.
const SchemaVersion = 1

type jsonDrift struct {
	Doc     string   `json:"doc"`
	Section string   `json:"section"`
	Line    int      `json:"line"`
	Files   []string `json:"files"`
}

type jsonReport struct {
	SchemaVersion int         `json:"schemaVersion"`
	Base          string      `json:"base"`
	Drifts        []jsonDrift `json:"drifts"`
}

// WriteJSON writes the drifts as indented JSON following the documented schema.
func WriteJSON(w io.Writer, base string, drifts []detect.Drift) error {
	out := jsonReport{SchemaVersion: SchemaVersion, Base: base, Drifts: make([]jsonDrift, 0, len(drifts))}
	for _, d := range drifts {
		files := d.Files
		if files == nil {
			files = []string{}
		}
		out.Drifts = append(out.Drifts, jsonDrift{Doc: d.Doc, Section: d.Section, Line: d.Line, Files: files})
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// WriteText writes one block per drift: the section with its location, then the
// triggering files. Nothing is written when there is no drift.
func WriteText(w io.Writer, drifts []detect.Drift) error {
	for _, d := range drifts {
		if _, err := fmt.Fprintf(w, "%s\n  %s:%d\n  changed code:\n", d.Section, d.Doc, d.Line); err != nil {
			return err
		}
		for _, f := range d.Files {
			if _, err := fmt.Fprintf(w, "    %s\n", f); err != nil {
				return err
			}
		}
	}
	return nil
}
