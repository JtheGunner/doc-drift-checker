package links_test

import (
	"reflect"
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/config"
	"github.com/JtheGunner/doc-drift-checker/internal/docparse"
	"github.com/JtheGunner/doc-drift-checker/internal/links"
)

var tracked = []string{
	"README.md",
	"cmd/app/main.go",
	"docs/guide.md",
	"internal/config/config.go",
	"internal/config/config_test.go",
	"internal/cli/root.go",
	"internal/cli/links.go",
}

func resolve(t *testing.T, doc string, cfg config.Config) links.DocLinks {
	t.Helper()
	return links.Resolve("README.md", docparse.ParseSections([]byte(doc)), cfg, tracked)
}

func section(t *testing.T, d links.DocLinks, title string) links.SectionLinks {
	t.Helper()
	for _, s := range d.Sections {
		if s.Heading == title {
			return s
		}
	}
	t.Fatalf("section %q not found in %+v", title, d.Sections)
	return links.SectionLinks{}
}

func TestAnnotationGlobsResolveAgainstTrackedFilesOnly(t *testing.T) {
	doc := "# T\n\n## Config\n\n<!-- drift: internal/config/*.go, cmd/**/*.go -->\n\ntext\n"
	got := section(t, resolve(t, doc, config.Defaults()), "Config")
	want := []links.Link{
		{File: "cmd/app/main.go", Origin: links.OriginAnnotation},
		{File: "internal/config/config.go", Origin: links.OriginAnnotation},
		{File: "internal/config/config_test.go", Origin: links.OriginAnnotation},
	}
	if !reflect.DeepEqual(got.Links, want) {
		t.Fatalf("got %+v\nwant %+v", got.Links, want)
	}
	if got.Unmapped || len(got.Missing) != 0 {
		t.Fatalf("unexpected status: %+v", got)
	}
}

func TestAnnotationGlobMatchingNothingIsMissing(t *testing.T) {
	doc := "# T\n\n## Config\n\n<!-- drift: internal/gone/*.go, internal/cli/root.go -->\n"
	got := section(t, resolve(t, doc, config.Defaults()), "Config")
	if want := []links.Link{{File: "internal/cli/root.go", Origin: links.OriginAnnotation}}; !reflect.DeepEqual(got.Links, want) {
		t.Fatalf("links: %+v", got.Links)
	}
	wantMissing := []links.Missing{{Ref: "internal/gone/*.go", Origin: links.OriginAnnotation}}
	if !reflect.DeepEqual(got.Missing, wantMissing) {
		t.Fatalf("missing: %+v", got.Missing)
	}
	if got.Unmapped {
		t.Fatal("a section with a link must not be unmapped")
	}
}

func TestConfigMappingByHeadingAndAnchor(t *testing.T) {
	doc := "# T\n\n## Usage Guide\n\ntext\n\n## Other\n\ntext\n"
	cfg := config.Defaults()
	cfg.Mappings = map[string][]string{
		"Usage Guide":  {"cmd/**"},
		"#usage-guide": {"internal/cli/*.go"},
		"#other":       {"nothing/here.go"},
	}
	d := resolve(t, doc, cfg)
	got := section(t, d, "Usage Guide")
	want := []links.Link{
		{File: "cmd/app/main.go", Origin: links.OriginConfig},
		{File: "internal/cli/links.go", Origin: links.OriginConfig},
		{File: "internal/cli/root.go", Origin: links.OriginConfig},
	}
	if !reflect.DeepEqual(got.Links, want) {
		t.Fatalf("got %+v", got.Links)
	}
	other := section(t, d, "Other")
	if len(other.Links) != 0 || len(other.Missing) != 1 || other.Missing[0].Origin != links.OriginConfig {
		t.Fatalf("config glob without match must be missing: %+v", other)
	}
}

func TestAnnotationAndConfigUnionAnnotationWinsOrigin(t *testing.T) {
	doc := "# T\n\n## S\n\n<!-- drift: internal/cli/root.go -->\n"
	cfg := config.Defaults()
	cfg.Mappings = map[string][]string{"S": {"internal/cli/*.go"}}
	got := section(t, resolve(t, doc, cfg), "S")
	want := []links.Link{
		{File: "internal/cli/links.go", Origin: links.OriginConfig},
		{File: "internal/cli/root.go", Origin: links.OriginAnnotation},
	}
	if !reflect.DeepEqual(got.Links, want) {
		t.Fatalf("got %+v", got.Links)
	}
}

func TestImplicitReferencesResolve(t *testing.T) {
	doc := "# T\n\n## S\n\nSee `internal/cli/root.go` and [guide](docs/guide.md#x) and `gone/file.go`.\n"
	got := section(t, resolve(t, doc, config.Defaults()), "S")
	want := []links.Link{
		{File: "docs/guide.md", Origin: links.OriginImplicit},
		{File: "internal/cli/root.go", Origin: links.OriginImplicit},
	}
	if !reflect.DeepEqual(got.Links, want) {
		t.Fatalf("links %+v", got.Links)
	}
	wantMissing := []links.Missing{{Ref: "gone/file.go", Origin: links.OriginImplicit, Line: 5}}
	if !reflect.DeepEqual(got.Missing, wantMissing) {
		t.Fatalf("missing %+v", got.Missing)
	}
}

func TestRelativeLinksResolveFromDocDirectory(t *testing.T) {
	doc := "# T\n\n## S\n\n[a](../cmd/app/main.go) [b](guide.md) [c](../../escape.go) [d](https://example.com/x.go)\n"
	d := links.Resolve("docs/intro.md", docparse.ParseSections([]byte(doc)), config.Defaults(), tracked)
	got := section(t, d, "S")
	want := []links.Link{
		{File: "cmd/app/main.go", Origin: links.OriginImplicit},
		{File: "docs/guide.md", Origin: links.OriginImplicit},
	}
	if !reflect.DeepEqual(got.Links, want) {
		t.Fatalf("links %+v", got.Links)
	}
	if len(got.Missing) != 1 || got.Missing[0].Ref != "../../escape.go" {
		t.Fatalf("missing %+v", got.Missing)
	}
}

func TestDirectoryReferenceLinksTrackedFilesBelow(t *testing.T) {
	doc := "# T\n\n## S\n\nThe `internal/config/` package.\n"
	got := section(t, resolve(t, doc, config.Defaults()), "S")
	if len(got.Links) != 2 || got.Links[0].File != "internal/config/config.go" {
		t.Fatalf("links %+v", got.Links)
	}
}

func TestExplicitOverridesImplicit(t *testing.T) {
	doc := "# T\n\n## S\n\nSee `internal/cli/root.go` and `gone.go`.\n\n<!-- drift: cmd/** -->\n"
	got := section(t, resolve(t, doc, config.Defaults()), "S")
	want := []links.Link{{File: "cmd/app/main.go", Origin: links.OriginAnnotation}}
	if !reflect.DeepEqual(got.Links, want) || len(got.Missing) != 0 {
		t.Fatalf("implicit links must be dropped: %+v", got)
	}
}

func TestConfigOverridesImplicit(t *testing.T) {
	doc := "# T\n\n## S\n\nSee `internal/cli/root.go`.\n"
	cfg := config.Defaults()
	cfg.Mappings = map[string][]string{"S": {"cmd/**"}}
	got := section(t, resolve(t, doc, cfg), "S")
	if len(got.Links) != 1 || got.Links[0].Origin != links.OriginConfig {
		t.Fatalf("links %+v", got.Links)
	}
}

func TestChildAnnotationAndReferencesDoNotMapParent(t *testing.T) {
	doc := "# T\n\n## Parent\n\nIntro without refs.\n\n### Child\n\nSee `internal/cli/root.go`.\n\n<!-- drift: cmd/** -->\n"
	d := resolve(t, doc, config.Defaults())
	parent := section(t, d, "Parent")
	if !parent.Unmapped || len(parent.Links) != 0 {
		t.Fatalf("parent must be unmapped: %+v", parent)
	}
	child := section(t, d, "Child")
	if len(child.Links) != 1 || child.Links[0].Origin != links.OriginAnnotation {
		t.Fatalf("child: %+v", child)
	}
}

func TestParentOwnTextBeforeChildStillMapsParent(t *testing.T) {
	doc := "# T\n\n## Parent\n\n`internal/cli/root.go`\n\n### Child\n\nx\n"
	parent := section(t, resolve(t, doc, config.Defaults()), "Parent")
	if len(parent.Links) != 1 {
		t.Fatalf("parent own reference lost: %+v", parent)
	}
}

func TestSectionWithoutLinksIsUnmapped(t *testing.T) {
	doc := "# T\n\n## S\n\nJust prose, mentions `someFunc()` and --flag only.\n"
	got := section(t, resolve(t, doc, config.Defaults()), "S")
	if !got.Unmapped || len(got.Links) != 0 || len(got.Missing) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestSectionWithOnlyMissingIsNotUnmapped(t *testing.T) {
	doc := "# T\n\n## S\n\nSee `gone/file.go`.\n"
	got := section(t, resolve(t, doc, config.Defaults()), "S")
	if got.Unmapped || len(got.Missing) != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestUnmappedPreambleIsOmitted(t *testing.T) {
	d := resolve(t, "Intro text.\n\n# T\n\ntext\n", config.Defaults())
	for _, s := range d.Sections {
		if s.Level == 0 {
			t.Fatalf("unmapped preamble should be omitted: %+v", s)
		}
	}
}

func TestAnnotationsInsideCodeDoNotCount(t *testing.T) {
	tests := []struct{ name, body string }{
		{"inline code span", "Use `<!-- drift: cmd/** -->` to annotate."},
		{"fenced code block", "```md\n<!-- drift: cmd/** -->\n```"},
		{"indented code block", "Prose.\n\n    <!-- drift: cmd/** -->\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := "# T\n\n## S\n\n" + tt.body + "\n\nSee `internal/cli/root.go`.\n"
			got := section(t, resolve(t, doc, config.Defaults()), "S")
			want := []links.Link{{File: "internal/cli/root.go", Origin: links.OriginImplicit}}
			if !reflect.DeepEqual(got.Links, want) || len(got.Missing) != 0 {
				t.Fatalf("code must not be an annotation, implicit links must stay: %+v", got)
			}
		})
	}
}

func TestAnnotationAsOwnCommentLineCounts(t *testing.T) {
	doc := "# T\n\n## S\n\n<!-- drift: cmd/** -->\n"
	got := section(t, resolve(t, doc, config.Defaults()), "S")
	if len(got.Links) != 1 || got.Links[0].Origin != links.OriginAnnotation {
		t.Fatalf("got %+v", got)
	}
}

func TestAnnotationAfterProseCounts(t *testing.T) {
	doc := "# T\n\n## S\n\nSome prose here. <!-- drift: cmd/** --> more prose.\n\nSee `internal/cli/root.go`.\n"
	got := section(t, resolve(t, doc, config.Defaults()), "S")
	want := []links.Link{{File: "cmd/app/main.go", Origin: links.OriginAnnotation}}
	if !reflect.DeepEqual(got.Links, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestSectionMetadata(t *testing.T) {
	d := resolve(t, "# T\n\n## S\n\ntext\n", config.Defaults())
	s := section(t, d, "S")
	if s.Anchor != "s" || s.Level != 2 || s.StartLine != 3 || s.EndLine != 5 || d.Doc != "README.md" {
		t.Fatalf("got %+v", s)
	}
}
