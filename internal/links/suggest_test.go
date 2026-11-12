package links_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/config"
	"github.com/JtheGunner/doc-drift-checker/internal/docparse"
	"github.com/JtheGunner/doc-drift-checker/internal/links"
)

func implicitSection(heading string, origins map[string]links.Origin) links.SectionLinks {
	sl := links.SectionLinks{Heading: heading, Level: 2, StartLine: 3, Links: []links.Link{}, Missing: []links.Missing{}}
	for file, origin := range origins {
		sl.Links = append(sl.Links, links.Link{File: file, Origin: origin})
	}
	return sl
}

func TestSuggest(t *testing.T) {
	many := map[string]links.Origin{}
	for i := 0; i < 11; i++ {
		many[fmt.Sprintf("src/f%02d.go", i)] = links.OriginImplicit
	}
	ten := map[string]links.Origin{}
	for i := 0; i < 10; i++ {
		ten[fmt.Sprintf("src/f%02d.go", i)] = links.OriginImplicit
	}
	implicit := links.OriginImplicit
	tests := []struct {
		name        string
		sec         links.SectionLinks
		wantFiles   []string
		wantSkipped bool
	}{
		{"single implicit", implicitSection("A", map[string]links.Origin{"src/a.go": implicit}), []string{"src/a.go"}, false},
		{"sorted", implicitSection("A", map[string]links.Origin{"src/b.go": implicit, "src/a.go": implicit}), []string{"src/a.go", "src/b.go"}, false},
		{"exactly ten", implicitSection("A", ten), sortedKeys(ten), false},
		{"eleven is too broad", implicitSection("A", many), nil, false},
		{"no links", implicitSection("A", nil), nil, false},
		{"annotation link", implicitSection("A", map[string]links.Origin{"src/a.go": links.OriginAnnotation}), nil, false},
		{"config link", implicitSection("A", map[string]links.Origin{"src/a.go": links.OriginConfig}), nil, false},
		{"mixed origins", implicitSection("A", map[string]links.Origin{"src/a.go": implicit, "src/b.go": links.OriginConfig}), nil, false},
		{"comma", implicitSection("A", map[string]links.Origin{"src/a,b.go": implicit}), nil, true},
		{"star", implicitSection("A", map[string]links.Origin{"src/*.go": implicit}), nil, true},
		{"question", implicitSection("A", map[string]links.Origin{"src/a?.go": implicit}), nil, true},
		{"brackets", implicitSection("A", map[string]links.Origin{"src/[a].go": implicit}), nil, true},
		{"braces", implicitSection("A", map[string]links.Origin{"src/{a}.go": implicit}), nil, true},
		{"comment close", implicitSection("A", map[string]links.Origin{"src/a-->b.go": implicit}), nil, true},
		{"comment open", implicitSection("A", map[string]links.Origin{"src/<!--.go": implicit}), nil, true},
		{"newline", implicitSection("A", map[string]links.Origin{"src/a\nb.go": implicit}), nil, true},
		{"control", implicitSection("A", map[string]links.Origin{"src/a\x1bb.go": implicit}), nil, true},
		{"trailing space", implicitSection("A", map[string]links.Origin{"src/a.go ": implicit}), nil, true},
		{"leading space", implicitSection("A", map[string]links.Origin{" src/a.go": implicit}), nil, true},
		{"backslash", implicitSection("A", map[string]links.Origin{`src\a.go`: implicit}), nil, true},
		{"one unsafe poisons the section", implicitSection("A", map[string]links.Origin{"src/a.go": implicit, "src/b*.go": implicit}), nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, skipped := links.Suggest(links.DocLinks{Doc: "README.md", Sections: []links.SectionLinks{tt.sec}})
			if tt.wantFiles == nil {
				if len(got) != 0 {
					t.Fatalf("suggested %+v, want none", got)
				}
			} else if len(got) != 1 || !reflect.DeepEqual(got[0].Files, tt.wantFiles) {
				t.Fatalf("suggestions = %+v, want files %v", got, tt.wantFiles)
			}
			if (len(skipped) > 0) != tt.wantSkipped {
				t.Errorf("skipped = %+v, want skipped=%v", skipped, tt.wantSkipped)
			}
		})
	}
}

func sortedKeys(m map[string]links.Origin) []string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	for i := range keys {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

func TestSuggestSkipsPreamble(t *testing.T) {
	sec := implicitSection("", map[string]links.Origin{"src/a.go": links.OriginImplicit})
	sec.Level = 0
	got, _ := links.Suggest(links.DocLinks{Sections: []links.SectionLinks{sec}})
	if len(got) != 0 {
		t.Errorf("suggested for preamble: %+v", got)
	}
}

func TestSuggestionComment(t *testing.T) {
	s := links.Suggestion{Files: []string{"a.go", "b/c.go"}}
	if got, want := s.Comment(), "<!-- drift: a.go, b/c.go -->"; got != want {
		t.Errorf("Comment() = %q, want %q", got, want)
	}
}

func TestInsertAnnotations(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"atx", "# T\n\n## Use\n\nSee `src/a.go`.\n", "# T\n\n## Use\n<!-- drift: src/a.go -->\n\nSee `src/a.go`.\n"},
		{"setext equals and dashes", "Use\n===\n\nSee `src/a.go`.\n\nSub\n---\n\nSee `src/a.go`.\n",
			"Use\n===\n<!-- drift: src/a.go -->\n\nSee `src/a.go`.\n\nSub\n---\n<!-- drift: src/a.go -->\n\nSee `src/a.go`.\n"},
		{"crlf", "## Use\r\n\r\nSee `src/a.go`.\r\n", "## Use\r\n<!-- drift: src/a.go -->\r\n\r\nSee `src/a.go`.\r\n"},
		{"heading on last line without newline", "## Use `src/a.go`", "## Use `src/a.go`\n<!-- drift: src/a.go -->"},
		{"closing hashes", "## Use ##\n\nSee `src/a.go`.\n", "## Use ##\n<!-- drift: src/a.go -->\n\nSee `src/a.go`.\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sections := docparse.ParseSections([]byte(tt.src))
			doc := links.Resolve("README.md", sections, config.Defaults(), []string{"README.md", "src/a.go"})
			sugg, _ := links.Suggest(doc)
			got, applied, skipped := links.InsertAnnotations([]byte(tt.src), sugg)
			if len(skipped) != 0 || len(applied) != len(sugg) {
				t.Fatalf("applied %d of %d, skipped %+v", len(applied), len(sugg), skipped)
			}
			if string(got) != tt.want {
				t.Errorf("got %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestInsertAnnotationsSkipsUndeterminedHeadings(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"heading in blockquote", "> ## Use\n> See `src/a.go`.\n"},
		{"lone CR line endings", "## Use\r\rSee `src/a.go`.\r"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sections := docparse.ParseSections([]byte(tt.src))
			doc := links.Resolve("README.md", sections, config.Defaults(), []string{"README.md", "src/a.go"})
			sugg, _ := links.Suggest(doc)
			got, applied, skipped := links.InsertAnnotations([]byte(tt.src), sugg)
			if string(got) != tt.src {
				t.Errorf("source changed: %q", got)
			}
			if len(applied) != 0 || len(skipped) != len(sugg) {
				t.Errorf("applied %v, skipped %v for %d suggestions", applied, skipped, len(sugg))
			}
		})
	}
}

func TestVerifyInserted(t *testing.T) {
	src := "## A\n\nSee `src/a.go`.\n\n## B\n\nSee `src/b.go`.\n"
	files := []string{"README.md", "src/a.go", "src/b.go"}
	resolve := func(s string) links.DocLinks {
		return links.Resolve("README.md", docparse.ParseSections([]byte(s)), config.Defaults(), files)
	}
	before := resolve(src)
	sugg, _ := links.Suggest(before)
	out, applied, _ := links.InsertAnnotations([]byte(src), sugg)
	if err := links.VerifyInserted(before, resolve(string(out)), applied); err != nil {
		t.Fatalf("VerifyInserted on a correct insertion: %v", err)
	}
	if err := links.VerifyInserted(before, before, applied); err == nil {
		t.Error("VerifyInserted accepted a document without annotations")
	}
	wrong := strings.Replace(string(out), "drift: src/a.go", "drift: src/b.go", 1)
	if err := links.VerifyInserted(before, resolve(wrong), applied); err == nil {
		t.Error("VerifyInserted accepted a wrong file set")
	}
}
