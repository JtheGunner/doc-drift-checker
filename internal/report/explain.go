package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"

	"github.com/JtheGunner/doc-drift-checker/internal/detect"
	"github.com/JtheGunner/doc-drift-checker/internal/lock"
	"github.com/JtheGunner/doc-drift-checker/internal/relevance"
)

// maxTextLine caps a single piece of repository-provided text in the text
// report, in characters.
const maxTextLine = 200

// WriteExplainText writes the explanation of one section as readable text.
// Every piece of text that comes from the repository (paths, commit subjects,
// headings, globs) is neutralised: control characters, including escape
// sequences, are shown as \xNN or \uNNNN and long values are cut.
func WriteExplainText(w io.Writer, ev detect.Evaluation) error {
	var b strings.Builder
	p := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	p("Section:  %s\n", clean(ev.Heading))
	p("Key:      %s\n", clean(ev.Key))
	p("Location: %s:%d (own lines %d-%d)\n", clean(ev.Doc), ev.Line, ev.OwnStart, ev.OwnEnd)
	if ev.Mode == detect.ModeDiff {
		p("Mode:     diff (base %s)\n", clean(ev.Base))
	} else {
		p("Mode:     history\n")
	}

	p("\nVerdict:  %s\n", verdictText(ev))
	p("Reason:   %s\n", reasonText(ev.Reason))
	if ev.Verdict == detect.VerdictDrift {
		p("Triggering files:\n")
		for _, f := range ev.Drift.Files {
			p("  %s\n", clean(f))
		}
		if len(ev.Drift.BrokenReferences) > 0 {
			p("Broken references:\n")
			for _, r := range ev.Drift.BrokenReferences {
				p("  %s %s (%s:%d)\n", r.Kind, clean(r.Value), clean(ev.Doc), r.Line)
			}
		}
	}

	writeLinksText(&b, ev)
	if len(ev.IgnoreRules) == 0 {
		p("\nIgnore rules: none\n")
	} else {
		p("\nIgnore rules:\n")
		for _, g := range ev.IgnoreRules {
			p("  %s\n", clean(g))
		}
	}

	p("\nFiles:\n")
	if len(ev.Files) == 0 {
		p("  none\n")
	}
	for _, f := range ev.Files {
		writeFileText(&b, ev, f)
	}

	writeOwnText(&b, ev)
	writeLockText(&b, ev.Lock)
	_, err := io.WriteString(w, b.String())
	return err
}

func verdictText(ev detect.Evaluation) string {
	if ev.Verdict != detect.VerdictDrift {
		return "no drift"
	}
	return "drift (severity " + severityOf(ev.Drift) + ")"
}

func writeLinksText(b *strings.Builder, ev detect.Evaluation) {
	b.WriteString("\nLinks:\n")
	if len(ev.Files) == 0 {
		b.WriteString("  none\n")
	}
	for _, f := range ev.Files {
		fmt.Fprintf(b, "  %s (%s)\n", clean(f.Path), f.Origin)
	}
	if len(ev.Missing) > 0 {
		b.WriteString("Unresolved references (match no tracked file):\n")
		for _, m := range ev.Missing {
			if m.Line > 0 {
				fmt.Fprintf(b, "  %s (%s, line %d)\n", clean(m.Ref), m.Origin, m.Line)
				continue
			}
			fmt.Fprintf(b, "  %s (%s)\n", clean(m.Ref), m.Origin)
		}
	}
	if ev.Unmapped {
		b.WriteString("The section has no links and no references to files.\n")
	}
}

func writeFileText(b *strings.Builder, ev detect.Evaluation, f detect.FileEvaluation) {
	fmt.Fprintf(b, "  %s (%s)\n", clean(f.Path), f.Origin)
	if ev.Mode == detect.ModeHistory {
		writeHistoryFileText(b, ev, f)
		return
	}
	if len(f.Changes) == 0 {
		b.WriteString("    unchanged between base and HEAD\n")
	}
	for _, c := range f.Changes {
		fmt.Fprintf(b, "    %s\n", changeText(c))
		fmt.Fprintf(b, "    relevance: %s\n", decisionText(c))
	}
	if len(f.Changes) > 0 {
		fmt.Fprintf(b, "    triggers drift: %s\n", yesNo(f.Triggers))
	}
	if len(f.Commits) == 0 {
		return
	}
	fmt.Fprintf(b, "    commits in %s..HEAD, newest first:\n", clean(ev.Base))
	for _, c := range f.Commits {
		fmt.Fprintf(b, "      %s %s %s\n", shortHash(c.Hash), formatTime(c.Time), clean(c.Subject))
	}
	if f.CommitsTruncated {
		b.WriteString("      (list truncated, older commits omitted)\n")
	}
}

func writeHistoryFileText(b *strings.Builder, ev detect.Evaluation, f detect.FileEvaluation) {
	switch {
	case f.IgnoredGlob != "":
		fmt.Fprintf(b, "    ignored: matches ignore glob %q, not dated\n", clean(f.IgnoredGlob))
	case f.Newest == nil:
		b.WriteString("    no commit counts (none, or all listed in the ignore-revs file)\n")
	default:
		fmt.Fprintf(b, "    newest commit: %s %s %s\n", shortHash(f.Newest.Hash), formatTime(f.Newest.Time), clean(f.Newest.Subject))
		if ev.SectionDate != nil {
			fmt.Fprintf(b, "    newer than the section: %s\n", yesNo(f.Triggers))
		}
	}
}

func changeText(c detect.FileChange) string {
	text := "status: " + statusWord(c.Status)
	if c.OldPath != "" {
		text += " from " + clean(c.OldPath) + " to " + clean(c.Path)
	}
	return text
}

func statusWord(status string) string {
	switch status {
	case "A":
		return "added"
	case "M":
		return "modified"
	case "D":
		return "deleted"
	case "R":
		return "renamed"
	case "C":
		return "copied"
	case "T":
		return "type changed"
	}
	return clean(status)
}

// decisionText names the relevance rule behind a decision.
func decisionText(c detect.FileChange) string {
	d := c.Decision
	switch d.Reason {
	case relevance.ReasonIgnoredPath:
		if d.OldGlob != "" {
			return fmt.Sprintf("dropped, the new path matches ignore glob %q and the old path matches %q", clean(d.Glob), clean(d.OldGlob))
		}
		return fmt.Sprintf("dropped, matches ignore glob %q", clean(d.Glob))
	case relevance.ReasonWhitespaceOnly:
		return "dropped, whitespace-only change"
	case relevance.ReasonAPIUnchanged:
		return fmt.Sprintf("dropped, exported API unchanged (analyzer %s)", clean(d.Analyzer))
	case relevance.ReasonAPIChanged:
		return fmt.Sprintf("kept, exported API changed (analyzer %s)", clean(d.Analyzer))
	case relevance.ReasonAnalyzerFailed:
		return fmt.Sprintf("kept, the %s analyzer failed so the change counts", clean(d.Analyzer))
	case relevance.ReasonRenameAcrossIgnore:
		if d.Glob != "" {
			return fmt.Sprintf("kept, rename or copy across the ignore boundary (new path matches %q, the old one does not)", clean(d.Glob))
		}
		return fmt.Sprintf("kept, rename or copy across the ignore boundary (old path matches %q, the new one does not)", clean(d.OldGlob))
	case relevance.ReasonNoAnalyzer:
		return "kept, no analyzer supports this language (path-based fallback)"
	case relevance.ReasonPathBased:
		return "kept, renames, copies and type changes are never dropped"
	}
	return clean(string(d.Reason))
}

func writeOwnText(b *strings.Builder, ev detect.Evaluation) {
	fmt.Fprintf(b, "\nSection's own lines (%d-%d):\n", ev.OwnStart, ev.OwnEnd)
	if ev.Mode == detect.ModeHistory {
		if ev.SectionDate == nil {
			b.WriteString("  no blame data\n")
			return
		}
		fmt.Fprintf(b, "  newest commit: %s %s\n", shortHash(ev.SectionDate.Hash), formatTime(ev.SectionDate.Time))
		return
	}
	fmt.Fprintf(b, "  document changed between base and HEAD: %s\n", yesNo(ev.DocChanged))
	if len(ev.OwnHunks) == 0 {
		b.WriteString("  changes touching these lines: none\n")
		return
	}
	b.WriteString("  changes touching these lines:\n")
	for _, h := range ev.OwnHunks {
		if h.Lines == 0 {
			fmt.Fprintf(b, "    text removed after line %d\n", h.Start)
			continue
		}
		fmt.Fprintf(b, "    lines %d-%d\n", h.Start, h.Start+h.Lines-1)
	}
}

func writeLockText(b *strings.Builder, l detect.LockEvaluation) {
	fmt.Fprintf(b, "\nLockfile %s:\n", lock.FileName)
	switch l.State {
	case detect.LockNone:
		b.WriteString("  no entry for this section\n")
		return
	case detect.LockMatches:
		b.WriteString("  entry matches the mapped code\n")
	default:
		b.WriteString("  entry differs from the mapped code in:\n")
		for _, f := range l.Differing {
			fmt.Fprintf(b, "    %s\n", clean(f))
		}
	}
	switch l.Effect {
	case detect.LockEffectSuppressed:
		b.WriteString("  effect: the drift is suppressed\n")
	case detect.LockEffectForcedDrift:
		b.WriteString("  effect: the section drifts, the differing files are its triggering files\n")
	default:
		b.WriteString("  effect: none\n")
	}
}

func reasonText(r detect.Reason) string {
	switch r {
	case detect.ReasonCodeChanged:
		return "a mapped file changed between base and HEAD and none of the section's own lines did"
	case detect.ReasonCodeNewer:
		return "a mapped file's newest commit is newer than the newest commit of the section's own lines"
	case detect.ReasonLockMismatch:
		return "the mapped code differs from the state acknowledged in " + lock.FileName + ", which decides in history mode"
	case detect.ReasonNoMappedCode:
		return "the section has no mapped code"
	case detect.ReasonNoFileChanged:
		return "no mapped file changed between base and HEAD"
	case detect.ReasonAllIgnored:
		return "all changes to mapped files are dropped by the relevance rules"
	case detect.ReasonOwnLinesChanged:
		return "the section's own lines changed"
	case detect.ReasonAcknowledged:
		return "acknowledged in " + lock.FileName + ", and the mapped code still matches"
	case detect.ReasonCodeNotNewer:
		return "no mapped file is newer than the section's own lines"
	case detect.ReasonNoDatedCode:
		return "no mapped file could be dated (all ignored, or no commit counts)"
	case detect.ReasonNoBlameData:
		return "the section's own lines have no blame data"
	}
	return clean(string(r))
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func shortHash(hash string) string {
	if len(hash) > 8 {
		hash = hash[:8]
	}
	return clean(hash)
}

func formatTime(unix int64) string {
	return time.Unix(unix, 0).UTC().Format(time.RFC3339)
}

// clean neutralises untrusted text for the terminal: invalid UTF-8 becomes
// U+FFFD, control and format characters become \xNN or \uNNNN escapes, and the
// result is cut to maxTextLine characters.
func clean(s string) string {
	s = strings.ToValidUTF8(s, "\uFFFD")
	var b strings.Builder
	count := 0
	for _, r := range s {
		if count == maxTextLine {
			b.WriteString("...")
			break
		}
		count++
		switch {
		case r < 0x100 && (unicode.IsControl(r)):
			fmt.Fprintf(&b, "\\x%02x", r)
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029':
			fmt.Fprintf(&b, "\\u%04x", r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// JSON explain report.

type jsonOwnRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type jsonExplainSection struct {
	Key      string       `json:"key"`
	Doc      string       `json:"doc"`
	Anchor   string       `json:"anchor"`
	Heading  string       `json:"heading"`
	Line     int          `json:"line"`
	OwnRange jsonOwnRange `json:"ownRange"`
}

type jsonLink struct {
	File   string `json:"file"`
	Origin string `json:"origin"`
}

type jsonUnresolved struct {
	Ref    string `json:"ref"`
	Origin string `json:"origin"`
	Line   int    `json:"line,omitempty"`
}

type jsonRelevance struct {
	Relevant bool   `json:"relevant"`
	Reason   string `json:"reason"`
	Glob     string `json:"glob,omitempty"`
	OldGlob  string `json:"oldGlob,omitempty"`
	Analyzer string `json:"analyzer,omitempty"`
}

type jsonFileChange struct {
	Status    string        `json:"status"`
	Path      string        `json:"path"`
	OldPath   string        `json:"oldPath,omitempty"`
	Relevance jsonRelevance `json:"relevance"`
}

type jsonCommit struct {
	Hash    string `json:"hash"`
	Date    string `json:"date"`
	Subject string `json:"subject,omitempty"`
}

type jsonExplainFile struct {
	Path             string           `json:"path"`
	Origin           string           `json:"origin"`
	Triggers         bool             `json:"triggers"`
	Changes          []jsonFileChange `json:"changes"`
	Commits          []jsonCommit     `json:"commits"`
	CommitsTruncated bool             `json:"commitsTruncated"`
	IgnoredGlob      string           `json:"ignoredGlob,omitempty"`
	NewestCommit     *jsonCommit      `json:"newestCommit"`
}

type jsonHunk struct {
	Start int `json:"start"`
	Lines int `json:"lines"`
}

type jsonOwn struct {
	DocChanged bool       `json:"docChanged"`
	Hunks      []jsonHunk `json:"hunks"`
}

type jsonLock struct {
	State             string   `json:"state"`
	Differing         []string `json:"differing"`
	Effect            string   `json:"effect"`
	DriftedBeforeLock bool     `json:"driftedBeforeLock"`
}

type jsonExplain struct {
	SchemaVersion    int                `json:"schemaVersion"`
	Mode             string             `json:"mode"`
	Base             string             `json:"base"`
	Section          jsonExplainSection `json:"section"`
	Verdict          string             `json:"verdict"`
	Reason           string             `json:"reason"`
	Severity         string             `json:"severity,omitempty"`
	TriggeringFiles  []string           `json:"triggeringFiles"`
	BrokenReferences []jsonReference    `json:"brokenReferences"`
	Links            []jsonLink         `json:"links"`
	Unresolved       []jsonUnresolved   `json:"unresolved"`
	Unmapped         bool               `json:"unmapped"`
	IgnoreRules      []string           `json:"ignoreRules"`
	Files            []jsonExplainFile  `json:"files"`
	Own              jsonOwn            `json:"own"`
	SectionDate      *jsonCommit        `json:"sectionDate"`
	Lock             jsonLock           `json:"lock"`
}

// WriteExplainJSON writes the explanation of one section as indented JSON
// following the documented explain schema.
func WriteExplainJSON(w io.Writer, ev detect.Evaluation) error {
	out := jsonExplain{
		SchemaVersion: SchemaVersion, Mode: string(ev.Mode), Base: ev.Base,
		Section: jsonExplainSection{
			Key: ev.Key, Doc: ev.Doc, Anchor: ev.Anchor, Heading: ev.Heading, Line: ev.Line,
			OwnRange: jsonOwnRange{Start: ev.OwnStart, End: ev.OwnEnd},
		},
		Verdict: string(ev.Verdict), Reason: string(ev.Reason),
		TriggeringFiles: []string{}, BrokenReferences: []jsonReference{},
		Links: []jsonLink{}, Unresolved: []jsonUnresolved{}, Unmapped: ev.Unmapped,
		IgnoreRules: append([]string{}, ev.IgnoreRules...), Files: []jsonExplainFile{},
		Own:  jsonOwn{DocChanged: ev.DocChanged, Hunks: []jsonHunk{}},
		Lock: jsonLock{State: string(ev.Lock.State), Differing: append([]string{}, ev.Lock.Differing...), Effect: string(ev.Lock.Effect), DriftedBeforeLock: ev.Lock.DriftedBeforeLock},
	}
	if ev.Verdict == detect.VerdictDrift {
		out.Severity = severityOf(ev.Drift)
		out.TriggeringFiles = append(out.TriggeringFiles, ev.Drift.Files...)
		for _, r := range ev.Drift.BrokenReferences {
			out.BrokenReferences = append(out.BrokenReferences, jsonReference{Kind: string(r.Kind), Value: r.Value, Line: r.Line})
		}
	}
	for _, f := range ev.Files {
		out.Links = append(out.Links, jsonLink{File: f.Path, Origin: string(f.Origin)})
		out.Files = append(out.Files, jsonFile(f))
	}
	for _, m := range ev.Missing {
		out.Unresolved = append(out.Unresolved, jsonUnresolved{Ref: m.Ref, Origin: string(m.Origin), Line: m.Line})
	}
	for _, h := range ev.OwnHunks {
		out.Own.Hunks = append(out.Own.Hunks, jsonHunk{Start: h.Start, Lines: h.Lines})
	}
	out.SectionDate = commitJSON(ev.SectionDate)

	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func jsonFile(f detect.FileEvaluation) jsonExplainFile {
	out := jsonExplainFile{
		Path: f.Path, Origin: string(f.Origin), Triggers: f.Triggers,
		Changes: []jsonFileChange{}, Commits: []jsonCommit{}, CommitsTruncated: f.CommitsTruncated,
		IgnoredGlob: f.IgnoredGlob, NewestCommit: commitJSON(f.Newest),
	}
	for _, c := range f.Changes {
		out.Changes = append(out.Changes, jsonFileChange{
			Status: c.Status, Path: c.Path, OldPath: c.OldPath,
			Relevance: jsonRelevance{
				Relevant: c.Decision.Relevant, Reason: string(c.Decision.Reason),
				Glob: c.Decision.Glob, OldGlob: c.Decision.OldGlob, Analyzer: c.Decision.Analyzer,
			},
		})
	}
	for _, c := range f.Commits {
		out.Commits = append(out.Commits, *commitJSON(&c))
	}
	return out
}

func commitJSON(c *detect.CommitRef) *jsonCommit {
	if c == nil {
		return nil
	}
	return &jsonCommit{Hash: c.Hash, Date: formatTime(c.Time), Subject: c.Subject}
}
