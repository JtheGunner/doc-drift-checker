package detect

import (
	"sort"
	"strings"

	"github.com/JtheGunner/doc-drift-checker/internal/config"
	"github.com/JtheGunner/doc-drift-checker/internal/docparse"
	"github.com/JtheGunner/doc-drift-checker/internal/links"
)

// MaxFileBytes is the largest file version that is searched. A mapped file
// with a larger version is unknown: it can neither prove a reference was
// removed nor present, so the section is never classed high because of it.
const MaxFileBytes = 4 << 20

// maxReferences bounds the references checked per section.
const maxReferences = 200

// minIdentifierLength is the shortest identifier (or dotted last segment) that
// is checked; shorter names match too much code to prove a removal.
const minIdentifierLength = 3

// FileVersion is a mapped code file at the merge base (Old) and at HEAD (New).
// Nil content means the file does not exist on that side (added or deleted).
// Unknown marks a file that could not be read in full (too large).
type FileVersion struct {
	Old, New []byte
	Unknown  bool
}

func (f FileVersion) unknown() bool {
	return f.Unknown || len(f.Old) > MaxFileBytes || len(f.New) > MaxFileBytes
}

// LinkedFiles returns the sorted, distinct files linked by the sections of the
// given drifts: the files whose versions Classify needs.
func LinkedFiles(docs []links.DocLinks, drifts []Drift) []string {
	index := sectionIndex(docs)
	seen := map[string]bool{}
	files := []string{}
	for _, d := range drifts {
		loc, ok := index[SectionKey{Doc: d.Doc, Line: d.Line}]
		if !ok {
			continue
		}
		for _, l := range loc.doc.Sections[loc.index].Links {
			if !seen[l.File] {
				seen[l.File] = true
				files = append(files, l.File)
			}
		}
	}
	sort.Strings(files)
	return files
}

type sectionLoc struct {
	doc   links.DocLinks
	index int
}

func sectionIndex(docs []links.DocLinks) map[SectionKey]sectionLoc {
	index := map[SectionKey]sectionLoc{}
	for _, doc := range docs {
		for i, s := range doc.Sections {
			index[SectionKey{Doc: doc.Doc, Line: s.StartLine}] = sectionLoc{doc: doc, index: i}
		}
	}
	return index
}

// Classify returns the drifts with Severity and BrokenReferences set; the
// input is not modified and the order is kept. Severity never adds or removes
// a drift: a section is high when at least one reference in its own text (child
// sections excluded) is broken, see BrokenReferences, and low otherwise.
//
// refs holds the extracted references per section; versions the mapped files by
// path. A linked file without a version entry exists on neither side and is
// skipped.
func Classify(docs []links.DocLinks, refs map[SectionKey][]docparse.Reference, versions map[string]FileVersion, drifts []Drift) []Drift {
	index := sectionIndex(docs)
	out := make([]Drift, 0, len(drifts))
	for _, d := range drifts {
		d.Severity = config.SeverityLow
		d.BrokenReferences = []docparse.Reference{}
		key := SectionKey{Doc: d.Doc, Line: d.Line}
		if loc, ok := index[key]; ok {
			start, end := OwnRange(loc.doc.Sections, loc.index)
			own := []docparse.Reference{}
			for _, r := range refs[key] {
				if r.Line >= start && r.Line <= end {
					own = append(own, r)
				}
			}
			files := []FileVersion{}
			for _, l := range loc.doc.Sections[loc.index].Links {
				if v, ok := versions[l.File]; ok {
					files = append(files, v)
				}
			}
			if broken := BrokenReferences(own, files); len(broken) > 0 {
				d.Severity = config.SeverityHigh
				d.BrokenReferences = broken
			}
		}
		out = append(out, d)
	}
	return out
}

// BrokenReferences returns the flag, env-var and identifier references that
// were present in the mapped files at the merge base and are present in none
// of them at HEAD, in input order, each kind and value once. Paths, links and
// commands are not checked. When any file is unknown nothing is claimed. See
// the token rules at tokenFound.
func BrokenReferences(refs []docparse.Reference, files []FileVersion) []docparse.Reference {
	broken := []docparse.Reference{}
	if len(files) == 0 {
		return broken
	}
	for _, f := range files {
		if f.unknown() {
			return broken
		}
	}
	type key struct {
		kind  docparse.ReferenceKind
		value string
	}
	oldSources := make([]string, len(files))
	newSources := make([]string, len(files))
	for i, f := range files {
		oldSources[i], newSources[i] = string(f.Old), string(f.New)
	}
	seen := map[key]bool{}
	checked := 0
	for _, r := range refs {
		k := key{r.Kind, r.Value}
		if seen[k] {
			continue
		}
		seen[k] = true
		tokens := tokensFor(r)
		if len(tokens) == 0 {
			continue
		}
		if checked++; checked > maxReferences {
			break
		}
		if foundIn(oldSources, tokens) && !foundIn(newSources, tokens) {
			broken = append(broken, r)
		}
	}
	return broken
}

type token struct {
	kind docparse.ReferenceKind
	text string
}

// tokensFor returns what to search for; none means the reference is not checked.
func tokensFor(r docparse.Reference) []token {
	switch r.Kind {
	case docparse.KindFlag:
		if len(r.Value) <= 2 {
			return nil
		}
		return []token{{r.Kind, r.Value}}
	case docparse.KindEnvVar:
		if r.Value == "" {
			return nil
		}
		return []token{{r.Kind, r.Value}}
	case docparse.KindIdentifier:
		name := strings.TrimSuffix(r.Value, "()")
		tokens := []token{}
		last := name
		if i := strings.LastIndex(name, "."); i >= 0 {
			last = name[i+1:]
			if !checkableName(last) {
				return nil
			}
			return append(tokens, token{r.Kind, name}, token{r.Kind, last})
		}
		if !checkableName(last) {
			return nil
		}
		return []token{{r.Kind, last}}
	}
	return nil
}

func checkableName(name string) bool {
	return len(name) >= minIdentifierLength && !commonWords[strings.ToLower(name)]
}

// commonWords are names too generic to prove a removal: they occur in almost
// any code.
var commonWords = map[string]bool{
	"run": true, "new": true, "get": true, "set": true, "name": true, "type": true,
	"value": true, "list": true, "string": true, "error": true, "init": true,
	"main": true, "open": true, "close": true, "read": true, "write": true,
	"start": true, "stop": true, "test": true, "data": true, "file": true,
	"path": true, "config": true, "help": true, "version": true, "default": true,
	"load": true, "save": true, "add": true, "remove": true, "delete": true,
	"update": true, "create": true, "build": true, "check": true, "print": true,
	"format": true, "parse": true, "status": true, "result": true, "option": true,
	"options": true, "args": true, "user": true, "key": true, "input": true,
	"output": true, "handler": true, "client": true, "server": true, "context": true,
	"message": true, "request": true, "response": true, "bool": true, "int": true,
	"len": true, "map": true,
}

// foundIn reports whether any token occurs in any file, on the old or new side.
func foundIn(sources []string, tokens []token) bool {
	for _, src := range sources {
		for _, t := range tokens {
			if tokenFound(src, t) {
				return true
			}
		}
	}
	return false
}

// tokenFound is a literal whole-token search (linear, no patterns built from
// document text):
//   - flag "--name": the text "--name" not preceded and not followed by a flag
//     name character (letter, digit, '-', '_'), or the bare name in matching
//     single quotes, double quotes or backticks (flag registration such as
//     Flags().Bool("name", ...)).
//   - env var and identifier: the exact text not adjacent to an identifier
//     character (letter, digit, '_', or a non-ASCII byte).
func tokenFound(s string, t token) bool {
	switch t.kind {
	case docparse.KindFlag:
		return occurs(s, t.text, isFlagChar) || quoted(s, strings.TrimPrefix(t.text, "--"))
	default:
		return occurs(s, t.text, isIdentChar)
	}
}

// occurs reports whether needle appears in s with no boundary character on
// either side.
func occurs(s, needle string, boundary func(byte) bool) bool {
	for from := 0; from <= len(s)-len(needle); {
		i := strings.Index(s[from:], needle)
		if i < 0 {
			return false
		}
		i += from
		end := i + len(needle)
		if (i == 0 || !boundary(s[i-1])) && (end == len(s) || !boundary(s[end])) {
			return true
		}
		from = i + 1
	}
	return false
}

// quoted reports whether name appears directly between two equal quote characters.
func quoted(s, name string) bool {
	for _, q := range []string{`"`, `'`, "`"} {
		if strings.Contains(s, q+name+q) {
			return true
		}
	}
	return false
}

func isFlagChar(c byte) bool { return c == '-' || isIdentChar(c) }

func isIdentChar(c byte) bool {
	return c == '_' || c >= 0x80 || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
