// Package links maps documentation sections to the code files they describe.
//
// Resolution is pure: Resolve takes parsed sections, the loaded config and the
// list of tracked files, and never touches git or the filesystem.
//
// Link sources, by Origin:
//
//   - annotation: an HTML comment "<!-- drift: glob, glob -->" in the section's own text
//   - config: Config.Mappings, keyed by heading text or anchor ("#usage")
//   - implicit: file-path and relative-link references found in the section's own text
//
// Explicit links (annotation and config) replace implicit ones. Annotation and
// config links are unioned; a file matched by both is reported once as annotation.
// Identifier, flag, env-var and command references are not resolved.
//
// A section's own text excludes its child sections.
package links

import (
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	mdtext "github.com/yuin/goldmark/text"

	"github.com/JtheGunner/doc-drift-checker/internal/config"
	"github.com/JtheGunner/doc-drift-checker/internal/docparse"
	pathglob "github.com/JtheGunner/doc-drift-checker/internal/glob"
)

// Origin tells where a link came from.
type Origin string

// The link origins.
const (
	OriginAnnotation Origin = "annotation"
	OriginConfig     Origin = "config"
	OriginImplicit   Origin = "implicit"
)

// Link connects a section to one tracked file.
type Link struct {
	File   string `json:"file"`
	Origin Origin `json:"origin"`
}

// Missing is a reference or glob that matches no tracked file. Line is the
// 1-based document line of an implicit reference; it is 0 for globs.
type Missing struct {
	Ref    string `json:"ref"`
	Origin Origin `json:"origin"`
	Line   int    `json:"line,omitempty"`
}

// SectionLinks is the resolved mapping of one section.
// Unmapped is true when the section has neither links nor missing references.
type SectionLinks struct {
	Heading   string    `json:"heading"`
	Anchor    string    `json:"anchor"`
	Level     int       `json:"level"`
	StartLine int       `json:"startLine"`
	EndLine   int       `json:"endLine"`
	Links     []Link    `json:"links"`
	Missing   []Missing `json:"missing"`
	Unmapped  bool      `json:"unmapped"`
}

// DocLinks is the resolved mapping of one documentation file.
type DocLinks struct {
	Doc      string         `json:"doc"`
	Sections []SectionLinks `json:"sections"`
}

var annotationPattern = regexp.MustCompile(`(?s)<!--\s*drift:(.*?)-->`)

// Resolve maps the sections of the document at docPath (slash-separated,
// relative to the repository root) to tracked files. Sections keep document
// order; links and missing entries are sorted. A preamble (level 0) section
// without any mapping is omitted.
func Resolve(docPath string, sections []docparse.Section, cfg config.Config, tracked []string) DocLinks {
	result := DocLinks{Doc: docPath, Sections: []SectionLinks{}}
	for i, s := range sections {
		own := ownSection(sections, i)
		sl := resolveSection(docPath, s, own, cfg, tracked)
		if s.Level == 0 && sl.Unmapped {
			continue
		}
		result.Sections = append(result.Sections, sl)
	}
	return result
}

// ownSection returns s with Text and EndLine cut before its first child section.
func ownSection(sections []docparse.Section, i int) docparse.Section {
	s := sections[i]
	for _, next := range sections[i+1:] {
		if next.StartLine > s.EndLine {
			break
		}
		if next.Level > s.Level {
			lines := strings.Split(s.Text, "\n")
			s.Text = strings.Join(lines[:next.StartLine-s.StartLine], "\n")
			s.EndLine = next.StartLine - 1
			break
		}
	}
	return s
}

func resolveSection(docPath string, full, own docparse.Section, cfg config.Config, tracked []string) SectionLinks {
	sl := SectionLinks{
		Heading: full.Title, Anchor: full.Anchor, Level: full.Level,
		StartLine: full.StartLine, EndLine: full.EndLine,
		Links: []Link{}, Missing: []Missing{},
	}

	byFile := map[string]Origin{}
	var missing []Missing
	// A section with any explicit link set (annotation or config) drops all
	// implicit links, even when every explicit glob matches nothing.
	explicit := false

	addGlobs := func(globs []string, origin Origin) {
		for _, glob := range globs {
			explicit = true
			matched := false
			for _, file := range tracked {
				if pathglob.Match(glob, file) {
					matched = true
					setOrigin(byFile, file, origin)
				}
			}
			if !matched {
				missing = append(missing, Missing{Ref: glob, Origin: origin})
			}
		}
	}
	addGlobs(annotationGlobs(own.Text), OriginAnnotation)
	addGlobs(configGlobs(cfg, full), OriginConfig)

	if !explicit {
		for _, ref := range docparse.ExtractReferences(own) {
			if ref.Kind != docparse.KindFilePath && ref.Kind != docparse.KindRelativeLink {
				continue
			}
			files := resolveReference(docPath, ref, tracked)
			if len(files) == 0 {
				missing = append(missing, Missing{Ref: ref.Value, Origin: OriginImplicit, Line: ref.Line})
			}
			for _, file := range files {
				setOrigin(byFile, file, OriginImplicit)
			}
		}
	}

	for file, origin := range byFile {
		sl.Links = append(sl.Links, Link{File: file, Origin: origin})
	}
	sort.Slice(sl.Links, func(i, j int) bool { return sl.Links[i].File < sl.Links[j].File })
	sl.Missing = dedupeMissing(missing)
	sl.Unmapped = len(sl.Links) == 0 && len(sl.Missing) == 0
	return sl
}

// setOrigin records origin for file; annotation beats config beats implicit.
func setOrigin(byFile map[string]Origin, file string, origin Origin) {
	if current, ok := byFile[file]; !ok || rank(origin) < rank(current) {
		byFile[file] = origin
	}
}

func rank(o Origin) int {
	switch o {
	case OriginAnnotation:
		return 0
	case OriginConfig:
		return 1
	default:
		return 2
	}
}

// annotationGlobs returns the globs of every "<!-- drift: ... -->" that is a
// real HTML comment: only HTML blocks and inline raw HTML count, never code
// spans, fenced code blocks or indented code blocks.
func annotationGlobs(text string) []string {
	src := []byte(text)
	root := goldmark.New().Parser().Parse(mdtext.NewReader(src))
	var globs []string
	collect := func(html string) {
		for _, m := range annotationPattern.FindAllStringSubmatch(html, -1) {
			for _, glob := range strings.Split(m[1], ",") {
				if glob = strings.TrimSpace(glob); glob != "" {
					globs = append(globs, glob)
				}
			}
		}
	}
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		var b strings.Builder
		switch v := n.(type) {
		case *ast.HTMLBlock:
			for i := 0; i < v.Lines().Len(); i++ {
				seg := v.Lines().At(i)
				b.Write(seg.Value(src))
			}
			collect(b.String())
		case *ast.RawHTML:
			for i := 0; i < v.Segments.Len(); i++ {
				seg := v.Segments.At(i)
				b.Write(seg.Value(src))
			}
			collect(b.String())
		}
		return ast.WalkContinue, nil
	})
	return globs
}

// configGlobs returns the mapped globs whose key is the section's heading
// text or its anchor ("#anchor"), in key order.
func configGlobs(cfg config.Config, s docparse.Section) []string {
	if s.Level == 0 {
		return nil
	}
	keys := make([]string, 0, len(cfg.Mappings))
	for key := range cfg.Mappings {
		if strings.TrimSpace(key) == s.Title || key == "#"+s.Anchor {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var globs []string
	for _, key := range keys {
		globs = append(globs, cfg.Mappings[key]...)
	}
	return globs
}

// resolveReference returns the tracked files a reference points at. A
// reference to a directory yields every tracked file below it. Relative links
// resolve against the document's directory, file paths against the repository
// root.
func resolveReference(docPath string, ref docparse.Reference, tracked []string) []string {
	target := ref.Value
	switch {
	case ref.Kind == docparse.KindRelativeLink && strings.HasPrefix(target, "/"):
		target = path.Clean(strings.TrimPrefix(target, "/"))
	case ref.Kind == docparse.KindRelativeLink:
		target = path.Join(path.Dir(docPath), target)
	default:
		target = path.Clean(target)
	}
	if target == "." || target == ".." || strings.HasPrefix(target, "../") {
		return nil
	}
	var files []string
	for _, file := range tracked {
		if file == target || strings.HasPrefix(file, target+"/") {
			files = append(files, file)
		}
	}
	return files
}

func dedupeMissing(missing []Missing) []Missing {
	out := []Missing{}
	seen := map[Missing]bool{}
	for _, m := range missing {
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Ref != out[j].Ref {
			return out[i].Ref < out[j].Ref
		}
		if out[i].Origin != out[j].Origin {
			return out[i].Origin < out[j].Origin
		}
		return out[i].Line < out[j].Line
	})
	return out
}
