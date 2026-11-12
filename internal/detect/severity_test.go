package detect_test

import (
	"strings"
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/config"
	"github.com/JtheGunner/doc-drift-checker/internal/detect"
	"github.com/JtheGunner/doc-drift-checker/internal/docparse"
	"github.com/JtheGunner/doc-drift-checker/internal/links"
)

func ref(kind docparse.ReferenceKind, value string) docparse.Reference {
	return docparse.Reference{Kind: kind, Value: value, Line: 5}
}

func version(oldSrc, newSrc string) detect.FileVersion {
	return detect.FileVersion{Old: []byte(oldSrc), New: []byte(newSrc)}
}

func brokenValues(refs []docparse.Reference, files ...detect.FileVersion) string {
	var values []string
	for _, r := range detect.BrokenReferences(refs, files) {
		values = append(values, string(r.Kind)+":"+r.Value)
	}
	return strings.Join(values, ",")
}

func TestBrokenReferences(t *testing.T) {
	flag := ref(docparse.KindFlag, "--verbose")
	env := ref(docparse.KindEnvVar, "APP_TOKEN")
	ident := func(v string) docparse.Reference { return ref(docparse.KindIdentifier, v) }
	one := func(r docparse.Reference) []docparse.Reference { return []docparse.Reference{r} }
	foo := ref(docparse.KindFlag, "--foo")

	tests := []struct {
		name  string
		refs  []docparse.Reference
		files []detect.FileVersion
		want  string
	}{
		{"flag removed", one(flag),
			[]detect.FileVersion{version(`fs.Bool("verbose", false, "")`, `fs.Bool("quiet", false, "")`)}, "flag:--verbose"},
		{"flag renamed, new name present", one(flag),
			[]detect.FileVersion{version("usage: --verbose\n", "usage: --chatty\n")}, "flag:--verbose"},
		{"flag still present", one(flag),
			[]detect.FileVersion{version("usage: --verbose\n", "usage: --verbose now\n")}, ""},
		{"flag still present as registered name", one(flag),
			[]detect.FileVersion{version(`Flags().Bool("verbose", false, "a")`, `Flags().Bool("verbose", true, "b")`)}, ""},
		{"flag registered with single quotes", one(flag),
			[]detect.FileVersion{version("--verbose", "'verbose'")}, ""},
		{"flag registered with backticks", one(flag),
			[]detect.FileVersion{version("--verbose", "`verbose`")}, ""},
		{"flag never present in old code", one(flag),
			[]detect.FileVersion{version("nothing here", "still nothing")}, ""},
		{"flag prefix does not count as present", one(foo),
			[]detect.FileVersion{version("--foo", "--foobar")}, "flag:--foo"},
		{"flag with dash suffix does not count as present", one(foo),
			[]detect.FileVersion{version("--foo", "--foo-bar")}, "flag:--foo"},
		{"flag followed by punctuation is present", one(foo),
			[]detect.FileVersion{version("--foo", "see (--foo), then")}, ""},
		{"quoted bare name needs matching quotes", one(foo),
			[]detect.FileVersion{version(`"foo"`, `"foobar" "xfoo" foo`)}, "flag:--foo"},
		{"flag at end of file is present", one(foo),
			[]detect.FileVersion{version("--foo", "--foo")}, ""},
		{"env var removed", one(env),
			[]detect.FileVersion{version(`os.Getenv("APP_TOKEN")`, `os.Getenv("APP_KEY")`)}, "env-var:APP_TOKEN"},
		{"env var still present", one(env),
			[]detect.FileVersion{version(`os.Getenv("APP_TOKEN")`, `os.LookupEnv("APP_TOKEN")`)}, ""},
		{"env var prefix does not count as present", one(ref(docparse.KindEnvVar, "FOO")),
			[]detect.FileVersion{version("FOO", "FOO_BAR and XFOO and FOO1")}, "env-var:FOO"},
		{"env var only longer name in old code is not the reference", one(ref(docparse.KindEnvVar, "FOO")),
			[]detect.FileVersion{version("FOO_BAR", "")}, ""},
		{"go identifier removed", one(ident("Execute")),
			[]detect.FileVersion{version("func Execute() {}", "func Perform() {}")}, "identifier:Execute"},
		{"go identifier renamed to longer name", one(ident("Execute")),
			[]detect.FileVersion{version("func Execute() {}", "func Executor() {}")}, "identifier:Execute"},
		{"go identifier still present", one(ident("Execute")),
			[]detect.FileVersion{version("func Execute() {}", "func Execute(a int) {}")}, ""},
		{"call suffix is ignored", one(ident("Execute()")),
			[]detect.FileVersion{version("func Execute() {}", "func Perform() {}")}, "identifier:Execute()"},
		{"identifier never present in old code", one(ident("Execute")),
			[]detect.FileVersion{version("func Other() {}", "func Another() {}")}, ""},
		{"short identifier is never broken", one(ident("Ab")),
			[]detect.FileVersion{version("Ab", "")}, ""},
		{"common word identifier is never broken", one(ident("Run")),
			[]detect.FileVersion{version("func Run() {}", "func Execute() {}")}, ""},
		{"dotted identifier: last segment still present", one(ident("Server.Execute")),
			[]detect.FileVersion{version("func (s *Server) Execute() {}", "func (s *Server) Execute() {}")}, ""},
		{"dotted identifier: last segment removed", one(ident("Server.Execute")),
			[]detect.FileVersion{version("func (s *Server) Execute() {}", "func (s *Server) Perform() {}")}, "identifier:Server.Execute"},
		{"dotted identifier: full text counts", one(ident("pkg.Execute")),
			[]detect.FileVersion{version("pkg.Execute()", "other.Execute()")}, ""},
		{"dotted identifier with common last segment is never broken", one(ident("Server.Run")),
			[]detect.FileVersion{version("Server.Run()", "")}, ""},
		{"reference present in a different mapped file", one(flag),
			[]detect.FileVersion{version("--verbose", "nothing"), version("other", "--verbose")}, ""},
		{"deleted mapped file held the reference", one(flag),
			[]detect.FileVersion{{Old: []byte("--verbose")}}, "flag:--verbose"},
		{"added mapped file does not make a reference broken", one(flag),
			[]detect.FileVersion{{New: []byte("--verbose")}}, ""},
		{"oversized file means unknown", one(flag),
			[]detect.FileVersion{{Old: []byte("--verbose"), Unknown: true}}, ""},
		{"unknown sibling file blocks the claim", one(flag),
			[]detect.FileVersion{version("--verbose", ""), {Unknown: true}}, ""},
		{"oversized content is unknown even when not flagged", one(flag),
			[]detect.FileVersion{{Old: []byte("--verbose" + strings.Repeat(" ", detect.MaxFileBytes)), New: []byte("x")}}, ""},
		{"paths, links and commands are not checked",
			[]docparse.Reference{ref(docparse.KindFilePath, "src/a.go"), ref(docparse.KindRelativeLink, "src/a.go"), ref(docparse.KindCommand, "make build")},
			[]detect.FileVersion{version("src/a.go make build", "")}, ""},
		{"duplicates are reported once", []docparse.Reference{flag, {Kind: docparse.KindFlag, Value: "--verbose", Line: 9}},
			[]detect.FileVersion{version("--verbose", "")}, "flag:--verbose"},
		{"no mapped files", one(flag), nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := brokenValues(tt.refs, tt.files...); got != tt.want {
				t.Fatalf("broken = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBrokenReferencesKeepsFirstLine(t *testing.T) {
	refs := []docparse.Reference{{Kind: docparse.KindFlag, Value: "--verbose", Line: 7}}
	got := detect.BrokenReferences(refs, []detect.FileVersion{version("--verbose", "")})
	if len(got) != 1 || got[0].Line != 7 {
		t.Fatalf("got %+v", got)
	}
}

func classifyFixture() ([]links.DocLinks, []detect.Drift) {
	docs := []links.DocLinks{{Doc: "README.md", Sections: []links.SectionLinks{
		{Heading: "Usage", Level: 2, StartLine: 3, EndLine: 12, Links: []links.Link{{File: "src/a.go"}}},
		{Heading: "Child", Level: 3, StartLine: 8, EndLine: 12, Links: []links.Link{{File: "src/a.go"}}},
	}}}
	drifts := []detect.Drift{
		{Doc: "README.md", Section: "Usage", Line: 3, Files: []string{"src/a.go"}},
		{Doc: "README.md", Section: "Child", Line: 8, Files: []string{"src/a.go"}},
	}
	return docs, drifts
}

func TestClassify(t *testing.T) {
	docs, drifts := classifyFixture()
	refs := map[detect.SectionKey][]docparse.Reference{
		// Usage's text includes the child's references; line 9 belongs to Child.
		{Doc: "README.md", Line: 3}: {
			{Kind: docparse.KindFlag, Value: "--gone", Line: 5},
			{Kind: docparse.KindFlag, Value: "--child-gone", Line: 9},
		},
		{Doc: "README.md", Line: 8}: {{Kind: docparse.KindFlag, Value: "--kept", Line: 9}},
	}
	versions := map[string]detect.FileVersion{"src/a.go": version("--gone --child-gone --kept", "--kept")}

	got := detect.Classify(docs, refs, versions, drifts)
	if got[0].Severity != config.SeverityHigh || len(got[0].BrokenReferences) != 1 || got[0].BrokenReferences[0].Value != "--gone" {
		t.Fatalf("Usage = %+v (child references must not count)", got[0])
	}
	if got[1].Severity != config.SeverityLow || got[1].BrokenReferences == nil || len(got[1].BrokenReferences) != 0 {
		t.Fatalf("Child = %+v", got[1])
	}
	if drifts[0].Severity != "" {
		t.Fatal("input drifts must not be mutated")
	}
}

func TestClassifyWithoutVersionsIsLow(t *testing.T) {
	docs, drifts := classifyFixture()
	refs := map[detect.SectionKey][]docparse.Reference{{Doc: "README.md", Line: 3}: {{Kind: docparse.KindFlag, Value: "--gone", Line: 5}}}
	got := detect.Classify(docs, refs, nil, drifts)
	for _, d := range got {
		if d.Severity != config.SeverityLow || len(d.BrokenReferences) != 0 {
			t.Fatalf("got %+v", d)
		}
	}
}

func TestLinkedFiles(t *testing.T) {
	docs, drifts := classifyFixture()
	docs[0].Sections[1].Links = append(docs[0].Sections[1].Links, links.Link{File: "src/0.go"})
	got := strings.Join(detect.LinkedFiles(docs, drifts), ",")
	if got != "src/0.go,src/a.go" {
		t.Fatalf("got %q", got)
	}
}

// withLowSeverity fills the severity fields Detect and History set on every drift.
func withLowSeverity(drifts []detect.Drift) []detect.Drift {
	out := make([]detect.Drift, len(drifts))
	for i, d := range drifts {
		d.Severity = config.SeverityLow
		d.BrokenReferences = []docparse.Reference{}
		out[i] = d
	}
	return out
}

func TestDetectAndHistoryDefaultToLow(t *testing.T) {
	docs, _ := classifyFixture()
	got := detect.Detect(detect.Input{Docs: docs, Changes: []detect.Change{{Path: "src/a.go"}}})
	if len(got) == 0 || got[0].Severity != config.SeverityLow || got[0].BrokenReferences == nil {
		t.Fatalf("diff drift = %+v", got)
	}
	hist := detect.History(detect.HistoryInput{
		Docs:         docs,
		SectionDates: map[detect.SectionKey]int64{{Doc: "README.md", Line: 3}: 1},
		FileDates:    map[string]int64{"src/a.go": 2},
	})
	if len(hist) == 0 || hist[0].Severity != config.SeverityLow || hist[0].BrokenReferences == nil {
		t.Fatalf("history drift = %+v", hist)
	}
}
