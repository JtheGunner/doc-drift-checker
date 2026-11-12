package docparse

import (
	"bytes"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// ReferenceKind classifies what a Reference points at.
type ReferenceKind string

// The kinds of references a section can contain.
const (
	// KindFilePath is a file or directory path written in inline code.
	KindFilePath ReferenceKind = "file-path"
	// KindRelativeLink is the target of a Markdown link that is neither
	// external nor an in-page anchor. Fragment and query are dropped.
	KindRelativeLink ReferenceKind = "relative-link"
	// KindIdentifier is a code identifier written in inline code.
	KindIdentifier ReferenceKind = "identifier"
	// KindFlag is a CLI flag such as --verbose. Values (--out=dir) are dropped.
	KindFlag ReferenceKind = "flag"
	// KindEnvVar is an environment variable name without "$" or braces.
	KindEnvVar ReferenceKind = "env-var"
	// KindCommand is a command line from a shell code block.
	KindCommand ReferenceKind = "command"
)

// Reference is a pointer from documentation prose to code, found in a section.
// Line is the 1-based line in the whole document, not in the section.
type Reference struct {
	Kind  ReferenceKind
	Value string
	Line  int
}

var (
	flagPattern       = regexp.MustCompile(`^--[A-Za-z][A-Za-z0-9-]*`)
	proseFlagPattern  = regexp.MustCompile(`(?:^|\s)(--[A-Za-z][A-Za-z0-9-]*)`)
	envDollarPattern  = regexp.MustCompile(`^\$(?:\{([A-Z_][A-Z0-9_]*)\}|([A-Z_][A-Z0-9_]*))$`)
	envNamePattern    = regexp.MustCompile(`^[A-Z][A-Z0-9]*(?:_[A-Z0-9]+)+$`)
	envAssignPattern  = regexp.MustCompile(`^([A-Z_][A-Z0-9_]*)=\S*$`)
	pathChars         = regexp.MustCompile(`^[\w./@+~-]+$`)
	lineSuffixPattern = regexp.MustCompile(`(?::\d+){1,2}$`)
	identifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*(?:\(\))?$`)
	schemePattern     = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)
)

var knownExtensions = map[string]bool{
	".go": true, ".md": true, ".yaml": true, ".yml": true, ".json": true,
	".toml": true, ".sh": true, ".js": true, ".ts": true, ".tsx": true,
	".jsx": true, ".py": true, ".rb": true, ".rs": true, ".java": true,
	".php": true, ".c": true, ".h": true, ".cpp": true, ".txt": true,
	".mod": true, ".sum": true, ".lock": true, ".env": true, ".ini": true,
	".cfg": true, ".conf": true, ".xml": true, ".html": true, ".css": true,
	".scss": true, ".sql": true, ".mk": true, ".proto": true, ".tf": true,
}

var knownFileNames = map[string]bool{"Makefile": true, "Dockerfile": true}

// slashWords are words commonly written as "a/b" in prose; a path made only of
// them is not a path.
var slashWords = map[string]bool{
	"and": true, "or": true, "yes": true, "no": true, "input": true,
	"output": true, "read": true, "write": true, "true": true, "false": true,
	"either": true, "on": true, "off": true, "in": true, "out": true,
}

var shellLanguages = map[string]bool{
	"sh": true, "bash": true, "shell": true, "zsh": true,
	"console": true, "shell-session": true, "sh-session": true,
}

// promptOnlyLanguages mix commands with output, so only prompt lines count.
var promptOnlyLanguages = map[string]bool{"console": true, "shell-session": true, "sh-session": true}

// ExtractReferences returns the code references found in s ordered by line.
// It is pure: it inspects only s.Text and never resolves a reference.
// Nested sections are part of their parent's text, so a parent reports the
// references of its children as well. Identical references on one line are
// reported once.
func ExtractReferences(s Section) []Reference {
	src := []byte(s.Text)
	root := goldmark.New().Parser().Parse(text.NewReader(src))
	e := &extractor{src: src, firstLine: s.StartLine, seen: map[Reference]bool{}}

	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch v := n.(type) {
		case *ast.CodeSpan:
			e.codeSpan(v)
			return ast.WalkSkipChildren, nil
		case *ast.Link:
			e.link(v)
		case *ast.Text:
			e.proseFlags(v)
		case *ast.FencedCodeBlock:
			e.shellBlock(v)
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})

	sort.SliceStable(e.refs, func(i, j int) bool { return e.refs[i].Line < e.refs[j].Line })
	return e.refs
}

type extractor struct {
	src       []byte
	firstLine int
	refs      []Reference
	seen      map[Reference]bool
}

func (e *extractor) add(kind ReferenceKind, value string, offset int) {
	r := Reference{Kind: kind, Value: value, Line: e.firstLine + bytes.Count(e.src[:offset], []byte("\n"))}
	if e.seen[r] {
		return
	}
	e.seen[r] = true
	e.refs = append(e.refs, r)
}

func (e *extractor) codeSpan(n *ast.CodeSpan) {
	offset, ok := nodeOffset(n)
	if !ok {
		return
	}
	var b strings.Builder
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if t, ok := c.(*ast.Text); ok {
			b.Write(t.Segment.Value(e.src))
		}
	}
	content := strings.TrimSpace(strings.ReplaceAll(b.String(), "\n", " "))
	tokens := strings.Fields(content)
	switch {
	case len(tokens) == 0:
		return
	case len(tokens) > 1:
		for _, tok := range tokens {
			if m := flagPattern.FindString(tok); m != "" && isWholeFlag(tok, m) {
				e.add(KindFlag, m, offset)
			}
		}
		return
	}

	if m := flagPattern.FindString(content); m != "" && isWholeFlag(content, m) {
		e.add(KindFlag, m, offset)
	} else if name := envVarName(content); name != "" {
		e.add(KindEnvVar, name, offset)
	} else if p := filePath(content); p != "" {
		e.add(KindFilePath, p, offset)
	} else if isIdentifier(content) {
		e.add(KindIdentifier, content, offset)
	}
}

// isWholeFlag reports whether tok is the flag m, optionally followed by =value.
func isWholeFlag(tok, m string) bool {
	rest := tok[len(m):]
	return rest == "" || strings.HasPrefix(rest, "=")
}

func (e *extractor) proseFlags(n *ast.Text) {
	seg := n.Segment
	for _, m := range proseFlagPattern.FindAllSubmatchIndex(seg.Value(e.src), -1) {
		flag := string(seg.Value(e.src)[m[2]:m[3]])
		e.add(KindFlag, flag, seg.Start+m[2])
	}
}

func (e *extractor) link(n *ast.Link) {
	dest := string(n.Destination)
	if dest == "" || strings.HasPrefix(dest, "#") || strings.HasPrefix(dest, "//") || schemePattern.MatchString(dest) {
		return
	}
	dest, _, _ = strings.Cut(dest, "#")
	dest, _, _ = strings.Cut(dest, "?")
	if dest == "" {
		return
	}
	offset, ok := nodeOffset(n)
	if !ok {
		offset, ok = blockOffset(n)
	}
	if !ok {
		return
	}
	e.add(KindRelativeLink, dest, offset)
}

func (e *extractor) shellBlock(n *ast.FencedCodeBlock) {
	if !shellLanguages[string(n.Language(e.src))] {
		return
	}
	promptOnly := promptOnlyLanguages[string(n.Language(e.src))]

	type codeLine struct {
		text   string
		offset int
	}
	var lines []codeLine
	segs := n.Lines()
	for i := 0; i < segs.Len(); i++ {
		seg := segs.At(i)
		lines = append(lines, codeLine{strings.TrimRight(string(seg.Value(e.src)), "\r\n"), seg.Start})
	}

	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i].text)
		hasPrompt := strings.HasPrefix(line, "$ ")
		if hasPrompt {
			line = strings.TrimSpace(line[2:])
		}
		if line == "" || strings.HasPrefix(line, "#") || (promptOnly && !hasPrompt) {
			continue
		}
		start := lines[i].offset
		for strings.HasSuffix(line, `\`) && i+1 < len(lines) {
			i++
			line = strings.TrimSpace(strings.TrimSuffix(line, `\`)) + " " + strings.TrimSpace(lines[i].text)
		}
		e.add(KindCommand, strings.TrimSpace(strings.TrimSuffix(line, `\`)), start)
	}
}

func envVarName(s string) string {
	if m := envDollarPattern.FindStringSubmatch(s); m != nil {
		return m[1] + m[2]
	}
	if envNamePattern.MatchString(s) {
		return s
	}
	if m := envAssignPattern.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}

// filePath returns the normalized path for s, or "" when s does not look like
// a path. Extensionless paths must have at least two segments, must not start
// with "/" (more likely a URL route) and must not be a pair of prose words.
func filePath(s string) string {
	s = lineSuffixPattern.ReplaceAllString(s, "")
	if !pathChars.MatchString(s) || strings.Contains(s, "//") {
		return ""
	}
	var segments []string
	for _, seg := range strings.Split(s, "/") {
		if seg != "" {
			segments = append(segments, seg)
		}
	}
	if len(segments) == 0 {
		return ""
	}
	base := segments[len(segments)-1]
	if knownExtensions[strings.ToLower(path.Ext(base))] || knownFileNames[base] {
		return s
	}
	if !strings.Contains(s, "/") {
		return ""
	}
	if strings.HasSuffix(s, "/") || strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") {
		return s
	}
	if strings.HasPrefix(s, "/") || len(segments) < 2 {
		return ""
	}
	allWords := true
	for _, seg := range segments {
		if isDigits(seg) {
			return ""
		}
		allWords = allWords && slashWords[strings.ToLower(seg)]
	}
	if allWords {
		return ""
	}
	return s
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// isIdentifier accepts names that carry a code signal: a call suffix, a dotted
// selector with a non-lowercase part, an underscore, or mixed case. Plain
// lowercase words, all-caps acronyms and numbers are prose.
func isIdentifier(s string) bool {
	if !identifierPattern.MatchString(s) {
		return false
	}
	if strings.HasSuffix(s, "()") {
		return true
	}
	hasLower := strings.ToUpper(s) != s
	hasUpper := strings.ToLower(s) != s
	switch {
	case strings.Contains(s, "."):
		return hasUpper
	case strings.Contains(s, "_"):
		return hasLower || hasUpper
	default:
		return hasLower && hasUpper
	}
}

// nodeOffset returns the source offset of the first text segment inside n.
func nodeOffset(n ast.Node) (int, bool) {
	if t, ok := n.(*ast.Text); ok {
		return t.Segment.Start, true
	}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if off, ok := nodeOffset(c); ok {
			return off, true
		}
	}
	return 0, false
}

// blockOffset returns the source offset of the closest enclosing block's first line.
func blockOffset(n ast.Node) (int, bool) {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if p.Type() == ast.TypeBlock && p.Lines().Len() > 0 {
			return p.Lines().At(0).Start, true
		}
	}
	return 0, false
}
