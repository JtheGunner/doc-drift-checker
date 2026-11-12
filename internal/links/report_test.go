package links_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/links"
)

func TestMatchDocs(t *testing.T) {
	got := links.MatchDocs([]string{"README.md", "docs/**/*.md"}, []string{"README.md", "docs/a.md", "docs/x/b.md", "src/c.md", "docs/d.txt"})
	want := []string{"README.md", "docs/a.md", "docs/x/b.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
}

func sampleReport() links.Report {
	return links.Report{Docs: []links.DocLinks{{
		Doc: "README.md",
		Sections: []links.SectionLinks{
			{Heading: "Usage", Anchor: "usage", Level: 2, StartLine: 3, EndLine: 9,
				Links:   []links.Link{{File: "cmd/main.go", Origin: links.OriginAnnotation}},
				Missing: []links.Missing{{Ref: "gone.go", Origin: links.OriginImplicit, Line: 5}}},
			{Heading: "Intro", Anchor: "intro", Level: 2, StartLine: 10, EndLine: 12,
				Links: []links.Link{}, Missing: []links.Missing{}, Unmapped: true},
		},
	}}}
}

func TestWriteText(t *testing.T) {
	var out bytes.Buffer
	if err := links.WriteText(&out, sampleReport()); err != nil {
		t.Fatal(err)
	}
	want := "README.md\n" +
		"  Usage (lines 3-9)\n" +
		"    cmd/main.go [annotation]\n" +
		"    missing: gone.go [implicit] (line 5)\n" +
		"  Intro (lines 10-12)\n" +
		"    unmapped\n"
	if out.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestWriteJSONSchema(t *testing.T) {
	var out bytes.Buffer
	if err := links.WriteJSON(&out, sampleReport()); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	docs := got["docs"].([]any)
	sec := docs[0].(map[string]any)["sections"].([]any)[0].(map[string]any)
	for _, key := range []string{"heading", "anchor", "level", "startLine", "endLine", "links", "missing", "unmapped"} {
		if _, ok := sec[key]; !ok {
			t.Errorf("missing key %q in %v", key, sec)
		}
	}
	link := sec["links"].([]any)[0].(map[string]any)
	if link["file"] != "cmd/main.go" || link["origin"] != "annotation" {
		t.Errorf("link: %v", link)
	}
}

func TestWriteJSONEmptyReportHasEmptyArray(t *testing.T) {
	var out bytes.Buffer
	if err := links.WriteJSON(&out, links.Report{Docs: []links.DocLinks{}}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "{\n  \"docs\": []\n}\n" {
		t.Fatalf("got %q", got)
	}
}
