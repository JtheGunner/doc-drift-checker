// Package docparse splits Markdown documents into sections. It is pure: it
// never touches the filesystem or git.
package docparse

import (
	"bytes"
	"strconv"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// Section is a heading-delimited part of a Markdown document.
//
// A section spans from its heading to the line before the next heading of the
// same or a higher level, so nested sections are part of their parent's range.
// Lines are 1-based and inclusive; trailing blank lines are excluded.
// Content before the first heading is reported as a Level 0 section with an
// empty Title and Anchor.
type Section struct {
	Title     string
	Anchor    string
	Level     int
	StartLine int
	EndLine   int
	Text      string
}

type heading struct {
	title string
	level int
	line  int
}

// ParseSections splits src into sections ordered by start line.
// Headings with an empty title cannot be located by the parser and are not
// reported as separate sections.
func ParseSections(src []byte) []Section {
	src = bytes.ReplaceAll(src, []byte("\r\n"), []byte("\n"))
	lines := strings.Split(string(src), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	headings := findHeadings(src)

	var sections []Section
	firstHeading := len(lines) + 1
	if len(headings) > 0 {
		firstHeading = headings[0].line
	}
	if s, ok := newSection(lines, "", "", 0, 1, firstHeading-1); ok {
		sections = append(sections, s)
	}

	anchors := map[string]int{}
	for i, h := range headings {
		end := len(lines)
		for _, next := range headings[i+1:] {
			if next.level <= h.level {
				end = next.line - 1
				break
			}
		}
		s, ok := newSection(lines, h.title, uniqueAnchor(anchors, h.title), h.level, h.line, end)
		if ok {
			sections = append(sections, s)
		}
	}
	return sections
}

func findHeadings(src []byte) []heading {
	root := goldmark.New().Parser().Parse(text.NewReader(src))
	var headings []heading
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		h, ok := n.(*ast.Heading)
		if !entering || !ok || h.Lines().Len() == 0 {
			return ast.WalkContinue, nil
		}
		offset := h.Lines().At(0).Start
		headings = append(headings, heading{
			title: plainText(h, src),
			level: h.Level,
			line:  bytes.Count(src[:offset], []byte("\n")) + 1,
		})
		return ast.WalkSkipChildren, nil
	})
	return headings
}

// plainText renders the text content of n without inline markup.
func plainText(n ast.Node, src []byte) string {
	var b strings.Builder
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch v := c.(type) {
		case *ast.Text:
			b.Write(v.Segment.Value(src))
			if v.SoftLineBreak() || v.HardLineBreak() {
				b.WriteByte(' ')
			}
		case *ast.String:
			b.Write(v.Value)
		default:
			b.WriteString(plainText(c, src))
		}
	}
	return strings.TrimSpace(b.String())
}

// newSection builds a section for the inclusive 1-based range, trimming blank
// trailing lines. It reports false when the range holds no content.
func newSection(lines []string, title, anchor string, level, start, end int) (Section, bool) {
	if end > len(lines) {
		end = len(lines)
	}
	for end >= start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	if level == 0 {
		for start <= end && strings.TrimSpace(lines[start-1]) == "" {
			start++
		}
	}
	if end < start {
		return Section{}, false
	}
	return Section{
		Title:     title,
		Anchor:    anchor,
		Level:     level,
		StartLine: start,
		EndLine:   end,
		Text:      strings.Join(lines[start-1:end], "\n"),
	}, true
}

// uniqueAnchor returns the GitHub-style slug of title, suffixed with -1, -2, ...
// for repeated slugs. seen tracks how often each base slug occurred.
func uniqueAnchor(seen map[string]int, title string) string {
	base := slug(title)
	n := seen[base]
	seen[base] = n + 1
	if n == 0 {
		return base
	}
	return base + "-" + strconv.Itoa(n)
}

func slug(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	return b.String()
}
