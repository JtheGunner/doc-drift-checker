package links

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	pathglob "github.com/JtheGunner/doc-drift-checker/internal/glob"
)

// Report is the resolved mapping of all documentation files. It is the
// document printed by "links --json":
//
//	{
//	  "docs": [
//	    {
//	      "doc": "README.md",
//	      "sections": [
//	        {
//	          "heading": "Usage", "anchor": "usage", "level": 2,
//	          "startLine": 3, "endLine": 9,
//	          "links":   [{"file": "cmd/main.go", "origin": "annotation"}],
//	          "missing": [{"ref": "gone.go", "origin": "implicit", "line": 5}],
//	          "unmapped": false
//	        }
//	      ]
//	    }
//	  ]
//	}
//
// origin is one of "annotation", "config", "implicit". Docs are sorted by
// path, sections keep document order, links are sorted by file and missing
// entries by ref. Arrays are never null.
type Report struct {
	Docs []DocLinks `json:"docs"`
}

// MatchDocs returns the tracked files matching any of the docs globs, sorted.
func MatchDocs(globs, tracked []string) []string {
	docs := []string{}
	for _, file := range tracked {
		for _, glob := range globs {
			if pathglob.Match(glob, file) {
				docs = append(docs, file)
				break
			}
		}
	}
	sort.Strings(docs)
	return docs
}

// WriteText prints the report grouped by doc file and section.
func WriteText(w io.Writer, r Report) error {
	for _, doc := range r.Docs {
		if _, err := fmt.Fprintf(w, "%s\n", doc.Doc); err != nil {
			return err
		}
		for _, s := range doc.Sections {
			if _, err := fmt.Fprintf(w, "  %s (lines %d-%d)\n", s.Heading, s.StartLine, s.EndLine); err != nil {
				return err
			}
			for _, l := range s.Links {
				if _, err := fmt.Fprintf(w, "    %s [%s]\n", l.File, l.Origin); err != nil {
					return err
				}
			}
			for _, m := range s.Missing {
				line := ""
				if m.Line > 0 {
					line = fmt.Sprintf(" (line %d)", m.Line)
				}
				if _, err := fmt.Fprintf(w, "    missing: %s [%s]%s\n", m.Ref, m.Origin, line); err != nil {
					return err
				}
			}
			if s.Unmapped {
				if _, err := fmt.Fprintln(w, "    unmapped"); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// WriteJSON prints the report as indented JSON.
func WriteJSON(w io.Writer, r Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}
