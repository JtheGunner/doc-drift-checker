package report_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/config"
	"github.com/JtheGunner/doc-drift-checker/internal/detect"
	"github.com/JtheGunner/doc-drift-checker/internal/docparse"
	"github.com/JtheGunner/doc-drift-checker/internal/links"
	"github.com/JtheGunner/doc-drift-checker/internal/relevance"
	"github.com/JtheGunner/doc-drift-checker/internal/report"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden file (run with -update to create it): %v", err)
	}
	if got != string(want) {
		t.Fatalf("output differs from %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

func textOf(t *testing.T, ev detect.Evaluation) string {
	t.Helper()
	var buf bytes.Buffer
	if err := report.WriteExplainText(&buf, ev); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func jsonOf(t *testing.T, ev detect.Evaluation) string {
	t.Helper()
	var buf bytes.Buffer
	if err := report.WriteExplainJSON(&buf, ev); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func keep(reason relevance.Reason) relevance.Decision {
	return relevance.Decision{Relevant: true, Reason: reason}
}

// diffDrift is a drifted section with every kind of file fact.
func diffDrift() detect.Evaluation {
	return detect.Evaluation{
		Key: "README.md#usage", Doc: "README.md", Anchor: "usage", Heading: "Usage",
		Line: 3, OwnStart: 3, OwnEnd: 6, Mode: detect.ModeDiff, Base: "main",
		Missing:     []links.Missing{{Ref: "gone/**", Origin: links.OriginAnnotation}, {Ref: "docs/x.md", Origin: links.OriginImplicit, Line: 5}},
		IgnoreRules: []string{"**/*_test.go", "vendor/**"},
		Files: []detect.FileEvaluation{
			{
				Path: "src/a.go", Origin: links.OriginAnnotation, Triggers: true,
				Changes: []detect.FileChange{{Status: "M", Path: "src/a.go", Decision: relevance.Decision{Relevant: true, Reason: relevance.ReasonAPIChanged, Analyzer: ".go"}}},
				Commits: []detect.CommitRef{
					{Hash: "0123456789abcdef0123456789abcdef01234567", Time: 1704110520, Subject: "change the API"},
					{Hash: "89abcdef0123456789abcdef0123456789abcdef", Time: 1704110460, Subject: "first edit"},
				},
				CommitsTruncated: true,
			},
			{
				Path: "src/b.go", Origin: links.OriginConfig,
				Changes: []detect.FileChange{{Status: "M", Path: "src/b.go", Decision: relevance.Decision{Reason: relevance.ReasonAPIUnchanged, Analyzer: ".go"}}},
			},
			{
				Path: "src/c_test.go", Origin: links.OriginImplicit,
				Changes: []detect.FileChange{{Status: "M", Path: "src/c_test.go", Decision: relevance.Decision{Reason: relevance.ReasonIgnoredPath, Glob: "**/*_test.go"}}},
			},
			{Path: "src/d.go", Origin: links.OriginImplicit, Changes: []detect.FileChange{}},
		},
		OwnHunks: []detect.LineRange{},
		Lock:     detect.LockEvaluation{State: detect.LockMismatch, Differing: []string{"src/a.go"}, Effect: detect.LockEffectNoEffect, DriftedBeforeLock: true},
		Verdict:  detect.VerdictDrift, Reason: detect.ReasonCodeChanged,
		Drift: detect.Drift{
			Doc: "README.md", Section: "Usage", Line: 3, Files: []string{"src/a.go"}, Severity: config.SeverityHigh,
			BrokenReferences: []docparse.Reference{{Kind: docparse.KindFlag, Value: "--gone", Line: 4}},
		},
	}
}

func historyNoDrift() detect.Evaluation {
	return detect.Evaluation{
		Key: "README.md#usage", Doc: "README.md", Anchor: "usage", Heading: "Usage",
		Line: 3, OwnStart: 3, OwnEnd: 6, Mode: detect.ModeHistory,
		IgnoreRules: []string{}, OwnHunks: []detect.LineRange{},
		Files: []detect.FileEvaluation{
			{Path: "src/a.go", Origin: links.OriginImplicit, Changes: []detect.FileChange{}, Newest: &detect.CommitRef{Hash: "aaaaaaaabbbbbbbb", Time: 1704110460, Subject: "code"}},
			{Path: "src/gen_test.go", Origin: links.OriginImplicit, Changes: []detect.FileChange{}, IgnoredGlob: "**/*_test.go"},
		},
		SectionDate: &detect.CommitRef{Hash: "ccccccccdddddddd", Time: 1704110520},
		Lock:        detect.LockEvaluation{State: detect.LockNone, Differing: []string{}, Effect: detect.LockEffectNone},
		Verdict:     detect.VerdictNoDrift, Reason: detect.ReasonCodeNotNewer,
	}
}

func TestExplainTextGolden(t *testing.T) {
	golden(t, "explain_diff_drift.golden", textOf(t, diffDrift()))
	golden(t, "explain_history_nodrift.golden", textOf(t, historyNoDrift()))
}

func TestExplainJSONGolden(t *testing.T) {
	golden(t, "explain_diff_drift.json.golden", jsonOf(t, diffDrift()))
	golden(t, "explain_history_nodrift.json.golden", jsonOf(t, historyNoDrift()))
}

func TestExplainJSONHasNoNullLists(t *testing.T) {
	// Evaluations built without any list (zero values) still encode lists as [].
	out := jsonOf(t, detect.Evaluation{Verdict: detect.VerdictNoDrift, Reason: detect.ReasonNoMappedCode, Mode: detect.ModeDiff})
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"triggeringFiles", "brokenReferences", "links", "unresolved", "ignoreRules", "files"} {
		list, ok := doc[key].([]any)
		if !ok || len(list) != 0 {
			t.Errorf("%s = %#v, want []", key, doc[key])
		}
	}
	lock := doc["lock"].(map[string]any)
	if list, ok := lock["differing"].([]any); !ok || len(list) != 0 {
		t.Errorf("lock.differing = %#v, want []", lock["differing"])
	}
	if doc["schemaVersion"] != float64(1) {
		t.Errorf("schemaVersion = %v", doc["schemaVersion"])
	}
	if _, present := doc["severity"]; present {
		t.Error("severity must be omitted without a drift")
	}
}

func TestExplainTextNamesEveryReason(t *testing.T) {
	tests := []struct {
		reason detect.Reason
		want   string
	}{
		{detect.ReasonCodeChanged, "a mapped file changed between base and HEAD and none of the section's own lines did"},
		{detect.ReasonCodeNewer, "newer than the newest commit of the section's own lines"},
		{detect.ReasonLockMismatch, "differs from the state acknowledged in doc-drift.lock"},
		{detect.ReasonNoMappedCode, "the section has no mapped code"},
		{detect.ReasonNoFileChanged, "no mapped file changed between base and HEAD"},
		{detect.ReasonAllIgnored, "all changes to mapped files are dropped by the relevance rules"},
		{detect.ReasonOwnLinesChanged, "the section's own lines changed"},
		{detect.ReasonAcknowledged, "acknowledged in doc-drift.lock"},
		{detect.ReasonCodeNotNewer, "no mapped file is newer than the section's own lines"},
		{detect.ReasonNoDatedCode, "no mapped file could be dated"},
		{detect.ReasonNoBlameData, "no blame data"},
	}
	for _, tt := range tests {
		t.Run(string(tt.reason), func(t *testing.T) {
			ev := historyNoDrift()
			ev.Reason = tt.reason
			if got := textOf(t, ev); !strings.Contains(got, tt.want) {
				t.Fatalf("output does not contain %q:\n%s", tt.want, got)
			}
		})
	}
}

func TestExplainTextNamesEveryRelevanceDecision(t *testing.T) {
	tests := []struct {
		name string
		d    relevance.Decision
		want string
	}{
		{"ignored glob", relevance.Decision{Reason: relevance.ReasonIgnoredPath, Glob: "vendor/**"}, `dropped, matches ignore glob "vendor/**"`},
		{"ignored rename", relevance.Decision{Reason: relevance.ReasonIgnoredPath, Glob: "gen/**", OldGlob: "**/*_test.go"}, `dropped, the new path matches ignore glob "gen/**" and the old path matches "**/*_test.go"`},
		{"whitespace", relevance.Decision{Reason: relevance.ReasonWhitespaceOnly}, "dropped, whitespace-only change"},
		{"api unchanged", relevance.Decision{Reason: relevance.ReasonAPIUnchanged, Analyzer: ".go"}, "dropped, exported API unchanged (analyzer .go)"},
		{"api changed", relevance.Decision{Relevant: true, Reason: relevance.ReasonAPIChanged, Analyzer: ".go"}, "kept, exported API changed (analyzer .go)"},
		{"analyzer failed", relevance.Decision{Relevant: true, Reason: relevance.ReasonAnalyzerFailed, Analyzer: ".go"}, "kept, the .go analyzer failed so the change counts"},
		{"rename into ignored", relevance.Decision{Relevant: true, Reason: relevance.ReasonRenameAcrossIgnore, Glob: "**/*_test.go"}, `kept, rename or copy across the ignore boundary (new path matches "**/*_test.go", the old one does not)`},
		{"rename out of ignored", relevance.Decision{Relevant: true, Reason: relevance.ReasonRenameAcrossIgnore, OldGlob: "gen/**"}, `kept, rename or copy across the ignore boundary (old path matches "gen/**", the new one does not)`},
		{"no analyzer", keep(relevance.ReasonNoAnalyzer), "kept, no analyzer supports this language (path-based fallback)"},
		{"path based", keep(relevance.ReasonPathBased), "kept, renames, copies and type changes are never dropped"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := diffDrift()
			ev.Files = []detect.FileEvaluation{{Path: "x.go", Origin: links.OriginImplicit, Changes: []detect.FileChange{{Status: "M", Path: "x.go", Decision: tt.d}}}}
			if got := textOf(t, ev); !strings.Contains(got, "relevance: "+tt.want) {
				t.Fatalf("output does not contain %q:\n%s", tt.want, got)
			}
		})
	}
}

func TestExplainTextShowsEveryOrigin(t *testing.T) {
	got := textOf(t, diffDrift())
	for _, want := range []string{"src/a.go (annotation)", "src/b.go (config)", "src/c_test.go (implicit)", "gone/** (annotation)", "docs/x.md (implicit, line 5)"} {
		if !strings.Contains(got, want) {
			t.Errorf("output does not contain %q", want)
		}
	}
}

func TestExplainTextUnmappedSection(t *testing.T) {
	ev := historyNoDrift()
	ev.Files, ev.Unmapped, ev.Reason = nil, true, detect.ReasonNoMappedCode
	got := textOf(t, ev)
	for _, want := range []string{"Links:\n  none", "The section has no links and no references to files.", "Files:\n  none"} {
		if !strings.Contains(got, want) {
			t.Errorf("output does not contain %q:\n%s", want, got)
		}
	}
}

func TestExplainTextNeutralisesControlCharacters(t *testing.T) {
	ev := diffDrift()
	ev.Heading = "Usage\x1b[2J"
	ev.Files[0].Commits[0].Subject = "evil\x1b[31m red\x1b]0;title\x07 \u202e text\r\nforged line"
	ev.Files[0].Path = "src/a\x00.go"
	ev.Drift.Files = []string{"src/a\x00.go"}
	got := textOf(t, ev)
	for _, r := range got {
		if r != '\n' && (r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || r == 0x202e) {
			t.Fatalf("output contains the control character %U:\n%q", r, got)
		}
	}
	for _, want := range []string{`evil\x1b[31m red\x1b]0;title\x07 \u202e text\x0d\x0aforged line`, `Usage\x1b[2J`, `src/a\x00.go`} {
		if !strings.Contains(got, want) {
			t.Errorf("output does not contain %q:\n%s", want, got)
		}
	}
	// JSON encodes the raw text; it is escaped by the encoder.
	var doc struct {
		Files []struct {
			Commits []struct{ Subject string } `json:"commits"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(jsonOf(t, ev)), &doc); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc.Files[0].Commits[0].Subject, "\x1b[31m") {
		t.Errorf("JSON subject = %q", doc.Files[0].Commits[0].Subject)
	}
}

func TestExplainTextCapsLongLines(t *testing.T) {
	ev := diffDrift()
	ev.Files[0].Commits[0].Subject = strings.Repeat("x", 5000)
	got := textOf(t, ev)
	if strings.Contains(got, strings.Repeat("x", 300)) {
		t.Fatal("a long subject must be cut")
	}
	if !strings.Contains(got, strings.Repeat("x", 200)+"...") {
		t.Fatal("a cut subject must end in an ellipsis")
	}
}
