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
	if err := links.WriteJSON(&out, links.Report{Docs: []links.DocLinks{}, UnmatchedMappings: []links.Missing{}}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "{\n  \"docs\": [],\n  \"unmatchedMappings\": []\n}\n" {
		t.Fatalf("got %q", got)
	}
}

func TestWriteTextNeutralisesRepositoryText(t *testing.T) {
	r := links.Report{Docs: []links.DocLinks{{
		Doc: "d\x1b[31m.md",
		Sections: []links.SectionLinks{{
			Heading: "H\x1b]0;x\x07", StartLine: 1, EndLine: 2,
			Links:   []links.Link{{File: "a\x1b[2J.go", Origin: links.OriginImplicit}},
			Missing: []links.Missing{{Ref: "m\r.go", Origin: links.OriginImplicit, Line: 1}},
		}},
	}}}
	var out bytes.Buffer
	if err := links.WriteText(&out, r); err != nil {
		t.Fatal(err)
	}
	want := "d\\x1b[31m.md\n  H\\x1b]0;x\\x07 (lines 1-2)\n    a\\x1b[2J.go [implicit]\n" +
		"    missing: m\\x0d.go [implicit] (line 1)\n"
	if out.String() != want {
		t.Fatalf("got:\n%q\nwant:\n%q", out.String(), want)
	}
}

func TestWriteUnmatchedMappings(t *testing.T) {
	r := links.Report{
		Docs:              []links.DocLinks{},
		UnmatchedMappings: []links.Missing{{Ref: "Fl\x1bags", Origin: links.OriginConfig}},
	}
	var text bytes.Buffer
	if err := links.WriteText(&text, r); err != nil {
		t.Fatal(err)
	}
	if want := "unmatched mapping: Fl\\x1bags [config]\n"; text.String() != want {
		t.Fatalf("got %q, want %q", text.String(), want)
	}
	var js bytes.Buffer
	if err := links.WriteJSON(&js, r); err != nil {
		t.Fatal(err)
	}
	var got struct {
		UnmatchedMappings []struct{ Ref, Origin string }
	}
	if err := json.Unmarshal(js.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.UnmatchedMappings) != 1 || got.UnmatchedMappings[0].Ref != "Fl\x1bags" || got.UnmatchedMappings[0].Origin != "config" {
		t.Fatalf("got %+v", got)
	}
}
