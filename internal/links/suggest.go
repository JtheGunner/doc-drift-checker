package links

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxSuggestedFiles is the largest number of linked files for which a section
// still counts as strongly linked. A section that mentions more files is too
// broad for a useful annotation.
const MaxSuggestedFiles = 10

// maxSetextLines bounds how far below a heading line the underline is looked for.
const maxSetextLines = 20

// Suggestion proposes an annotation for one section: its implicit links, made
// explicit. Line is the 1-based line of the heading in the document.
type Suggestion struct {
	Heading string
	Anchor  string
	Level   int
	Line    int
	Files   []string
}

// Comment returns the annotation text for the suggestion.
func (s Suggestion) Comment() string {
	return "<!-- drift: " + strings.Join(s.Files, ", ") + " -->"
}

// Skipped is a section that qualified for an annotation but was not suggested
// or not written, with the reason.
type Skipped struct {
	Heading string
	Line    int
	Reason  string
}

// Suggest proposes annotations for the sections of doc whose links are all
// implicit and number between 1 and MaxSuggestedFiles. Sections with an
// explicit link, no link or too many links are not suggested. A section with a
// file name that cannot be written safely into an annotation comment is
// reported as skipped instead. Suggestions keep document order.
func Suggest(doc DocLinks) ([]Suggestion, []Skipped) {
	var suggestions []Suggestion
	var skipped []Skipped
	for _, s := range doc.Sections {
		if s.Level == 0 || len(s.Links) == 0 || len(s.Links) > MaxSuggestedFiles {
			continue
		}
		files, onlyImplicit := implicitFiles(s)
		if !onlyImplicit {
			continue
		}
		if file, ok := firstUnsafe(files); ok {
			skipped = append(skipped, Skipped{
				Heading: s.Heading, Line: s.StartLine,
				Reason: fmt.Sprintf("file name %q cannot be written safely into an annotation", file),
			})
			continue
		}
		suggestions = append(suggestions, Suggestion{
			Heading: s.Heading, Anchor: s.Anchor, Level: s.Level, Line: s.StartLine, Files: files,
		})
	}
	return suggestions, skipped
}

func implicitFiles(s SectionLinks) ([]string, bool) {
	files := make([]string, 0, len(s.Links))
	for _, l := range s.Links {
		if l.Origin != OriginImplicit {
			return nil, false
		}
		files = append(files, l.File)
	}
	sort.Strings(files)
	return files, true
}

func firstUnsafe(files []string) (string, bool) {
	for _, f := range files {
		if !safeInAnnotation(f) {
			return f, true
		}
	}
	return "", false
}

// safeInAnnotation reports whether file can be written verbatim into an
// annotation and be read back as exactly that one glob.
func safeInAnnotation(file string) bool {
	if file == "" || !utf8.ValidString(file) {
		return false
	}
	if strings.Contains(file, "-->") || strings.Contains(file, "<!--") {
		return false
	}
	first, _ := utf8.DecodeRuneInString(file)
	last, _ := utf8.DecodeLastRuneInString(file)
	if unicode.IsSpace(first) || unicode.IsSpace(last) {
		return false
	}
	for _, r := range file {
		if unicode.IsControl(r) || strings.ContainsRune("*?[]{},\\", r) {
			return false
		}
	}
	return true
}

// InsertAnnotations adds each suggestion as its own line directly after its
// heading (after the underline for a Setext heading) and returns the new
// source with the applied suggestions. A suggestion whose heading cannot be
// located with certainty is reported as skipped and leaves the source alone.
// Line endings follow the heading line; a file with a lone CR as line ending
// is never changed.
func InsertAnnotations(src []byte, suggestions []Suggestion) ([]byte, []Suggestion, []Skipped) {
	lines := splitLines(string(src))
	if hasLoneCR(lines) {
		return src, nil, skipAll(suggestions, "the file uses CR line endings")
	}
	defaultEOL := "\n"
	if strings.Contains(string(src), "\r\n") {
		defaultEOL = "\r\n"
	}
	insertAfter := map[int]string{} // 0-based line index -> text to insert after it
	var applied []Suggestion
	var skipped []Skipped
	for _, s := range suggestions {
		idx, ok := headingEnd(lines, s)
		if !ok {
			skipped = append(skipped, Skipped{Heading: s.Heading, Line: s.Line, Reason: "heading kind could not be determined safely"})
			continue
		}
		eol := lineEnding(lines[idx])
		if eol == "" {
			insertAfter[idx] = defaultEOL + s.Comment()
		} else {
			insertAfter[idx] = s.Comment() + eol
		}
		applied = append(applied, s)
	}
	if len(applied) == 0 {
		return src, nil, skipped
	}
	var b strings.Builder
	for i, line := range lines {
		b.WriteString(line)
		b.WriteString(insertAfter[i])
	}
	return []byte(b.String()), applied, skipped
}

func skipAll(suggestions []Suggestion, reason string) []Skipped {
	var skipped []Skipped
	for _, s := range suggestions {
		skipped = append(skipped, Skipped{Heading: s.Heading, Line: s.Line, Reason: reason})
	}
	return skipped
}

// splitLines splits s after every LF, keeping the line endings.
func splitLines(s string) []string {
	lines := strings.SplitAfter(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func lineEnding(line string) string {
	switch {
	case strings.HasSuffix(line, "\r\n"):
		return "\r\n"
	case strings.HasSuffix(line, "\n"):
		return "\n"
	}
	return ""
}

func lineContent(line string) string {
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
}

func hasLoneCR(lines []string) bool {
	for _, l := range lines {
		if strings.Contains(lineContent(l), "\r") {
			return true
		}
	}
	return false
}

// headingEnd returns the 0-based index of the last line of the heading of s.
func headingEnd(lines []string, s Suggestion) (int, bool) {
	start := s.Line - 1
	if start < 0 || start >= len(lines) {
		return 0, false
	}
	content := lineContent(lines[start])
	if isATX(content, s.Level) {
		return start, true
	}
	if !plausibleSetextText(content) {
		return 0, false
	}
	for k := start + 1; k < len(lines) && k <= start+maxSetextLines; k++ {
		next := lineContent(lines[k])
		if strings.TrimSpace(next) == "" {
			return 0, false
		}
		if level := setextLevel(next); level != 0 {
			return k, level == s.Level
		}
	}
	return 0, false
}

func trimIndent(s string) (string, bool) {
	n := 0
	for n < len(s) && s[n] == ' ' {
		n++
	}
	return s[n:], n <= 3
}

func isATX(content string, level int) bool {
	rest, ok := trimIndent(content)
	if !ok {
		return false
	}
	n := 0
	for n < len(rest) && rest[n] == '#' {
		n++
	}
	if n != level || n > 6 {
		return false
	}
	return n == len(rest) || rest[n] == ' ' || rest[n] == '\t'
}

// plausibleSetextText rejects first lines that start a block other than a
// paragraph (quote, list, code, HTML), where an underline would not make a heading.
func plausibleSetextText(content string) bool {
	rest, ok := trimIndent(content)
	if !ok || rest == "" {
		return false
	}
	switch rest[0] {
	case '>', '-', '+', '*', '<', '\t', '#', '`', '~', '|', '=':
		return false
	}
	digits := 0
	for digits < len(rest) && rest[digits] >= '0' && rest[digits] <= '9' {
		digits++
	}
	if digits > 0 && digits < len(rest) && (rest[digits] == '.' || rest[digits] == ')') {
		return false
	}
	return true
}

// setextLevel returns 1 for a "===" underline, 2 for "---" and 0 otherwise.
func setextLevel(content string) int {
	rest, ok := trimIndent(content)
	if !ok {
		return 0
	}
	rest = strings.TrimRight(rest, " \t")
	if rest == "" {
		return 0
	}
	switch {
	case strings.Trim(rest, "=") == "":
		return 1
	case strings.Trim(rest, "-") == "":
		return 2
	}
	return 0
}

// VerifyInserted checks that after, the resolution of the document after
// InsertAnnotations, differs from before exactly as intended: the same
// sections, every applied section now linked by annotation to exactly the
// suggested files, and every other section with unchanged links.
func VerifyInserted(before, after DocLinks, applied []Suggestion) error {
	if len(before.Sections) != len(after.Sections) {
		return fmt.Errorf("%s: section count changed from %d to %d", before.Doc, len(before.Sections), len(after.Sections))
	}
	byLine := map[int]Suggestion{}
	for _, s := range applied {
		byLine[s.Line] = s
	}
	for i, b := range before.Sections {
		a := after.Sections[i]
		if a.Heading != b.Heading || a.Anchor != b.Anchor || a.Level != b.Level {
			return fmt.Errorf("%s: section %q changed", before.Doc, b.Heading)
		}
		s, written := byLine[b.StartLine]
		if !written {
			if !reflect.DeepEqual(a.Links, b.Links) || !sameMissing(a.Missing, b.Missing) {
				return fmt.Errorf("%s: links of section %q changed", before.Doc, b.Heading)
			}
			continue
		}
		if len(a.Missing) != 0 || len(a.Links) != len(s.Files) {
			return fmt.Errorf("%s: section %q does not resolve to the suggested files", before.Doc, b.Heading)
		}
		for j, l := range a.Links {
			if l.Origin != OriginAnnotation || l.File != s.Files[j] {
				return fmt.Errorf("%s: section %q does not resolve to the suggested files", before.Doc, b.Heading)
			}
		}
	}
	return nil
}

// sameMissing compares unresolved references without their line numbers,
// which shift when lines are inserted above them.
func sameMissing(a, b []Missing) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Ref != b[i].Ref || a[i].Origin != b[i].Origin {
			return false
		}
	}
	return true
}
