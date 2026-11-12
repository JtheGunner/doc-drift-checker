package detect_test

import (
	"reflect"
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/detect"
	"github.com/JtheGunner/doc-drift-checker/internal/docparse"
	"github.com/JtheGunner/doc-drift-checker/internal/links"
	"github.com/JtheGunner/doc-drift-checker/internal/relevance"
)

var (
	usageKey = detect.SectionKey{Doc: "README.md", Line: 3}
	flagsKey = detect.SectionKey{Doc: "README.md", Line: 7}
)

func kept(path string) detect.FileChange {
	return detect.FileChange{Status: "M", Path: path, Decision: relevance.Decision{Relevant: true, Reason: relevance.ReasonNoAnalyzer}}
}

func dropped(path string, d relevance.Decision) detect.FileChange {
	return detect.FileChange{Status: "M", Path: path, Decision: d}
}

func evalOf(t *testing.T, evals detect.Evaluations, key string) detect.Evaluation {
	t.Helper()
	e, ok := evals.Find(key)
	if !ok {
		t.Fatalf("no evaluation for %q", key)
	}
	return e
}

func TestEvaluateDiff(t *testing.T) {
	tests := []struct {
		name        string
		changes     []detect.FileChange
		docChanges  map[string][]detect.LineRange
		lock        map[detect.SectionKey][]string
		key         detect.SectionKey
		wantVerdict detect.Verdict
		wantReason  detect.Reason
		wantEffect  detect.LockEffect
	}{
		{name: "code changed", changes: []detect.FileChange{kept("a.go")}, key: usageKey, wantVerdict: detect.VerdictDrift, wantReason: detect.ReasonCodeChanged, wantEffect: detect.LockEffectNone},
		{name: "nothing changed", key: usageKey, wantVerdict: detect.VerdictNoDrift, wantReason: detect.ReasonNoFileChanged, wantEffect: detect.LockEffectNone},
		{
			name: "own lines changed", changes: []detect.FileChange{kept("a.go")},
			docChanges: map[string][]detect.LineRange{"README.md": {{Start: 4, Lines: 1}}},
			key:        usageKey, wantVerdict: detect.VerdictNoDrift, wantReason: detect.ReasonOwnLinesChanged, wantEffect: detect.LockEffectNone,
		},
		{
			name:    "every change dropped by relevance",
			changes: []detect.FileChange{dropped("a.go", relevance.Decision{Reason: relevance.ReasonIgnoredPath, Glob: "**/*.go"})},
			key:     usageKey, wantVerdict: detect.VerdictNoDrift, wantReason: detect.ReasonAllIgnored, wantEffect: detect.LockEffectNone,
		},
		{
			name: "acknowledged state suppresses", changes: []detect.FileChange{kept("a.go")},
			lock: map[detect.SectionKey][]string{usageKey: {}},
			key:  usageKey, wantVerdict: detect.VerdictNoDrift, wantReason: detect.ReasonAcknowledged, wantEffect: detect.LockEffectSuppressed,
		},
		{
			name: "mismatching entry has no effect in diff mode", changes: []detect.FileChange{kept("a.go")},
			lock: map[detect.SectionKey][]string{usageKey: {"a.go"}},
			key:  usageKey, wantVerdict: detect.VerdictDrift, wantReason: detect.ReasonCodeChanged, wantEffect: detect.LockEffectNoEffect,
		},
		{
			name: "matching entry without drift has no effect", key: usageKey,
			lock:        map[detect.SectionKey][]string{usageKey: {}},
			wantVerdict: detect.VerdictNoDrift, wantReason: detect.ReasonNoFileChanged, wantEffect: detect.LockEffectNoEffect,
		},
		{name: "section without links", key: detect.SectionKey{Doc: "README.md", Line: 1}, wantVerdict: detect.VerdictNoDrift, wantReason: detect.ReasonNoMappedCode, wantEffect: detect.LockEffectNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := detect.EvalInput{Mode: detect.ModeDiff, Base: "main", Docs: evalDocs(), Changes: tt.changes, DocChanges: tt.docChanges, Lock: tt.lock}
			evals := detect.Evaluate(in)
			e := evalOf(t, evals, "README.md#"+anchorAt(tt.key.Line))
			if e.Verdict != tt.wantVerdict || e.Reason != tt.wantReason || e.Lock.Effect != tt.wantEffect {
				t.Fatalf("verdict/reason/effect = %s/%s/%s, want %s/%s/%s", e.Verdict, e.Reason, e.Lock.Effect, tt.wantVerdict, tt.wantReason, tt.wantEffect)
			}
		})
	}
}

// anchorAt maps a section line of evalDocs() to its anchor.
func anchorAt(line int) string {
	return map[int]string{1: "project", 3: "usage", 7: "flags", 11: "other"}[line]
}

func TestEvaluateDiffRecordsFacts(t *testing.T) {
	renamed := detect.FileChange{Status: "R", Path: "new/a.go", OldPath: "a.go", Decision: relevance.Decision{Relevant: true, Reason: relevance.ReasonPathBased}}
	in := detect.EvalInput{
		Mode: detect.ModeDiff, Base: "main", Docs: evalDocs(), IgnoreRules: []string{"vendor/**"},
		Changes:    []detect.FileChange{renamed, dropped("b.go", relevance.Decision{Reason: relevance.ReasonWhitespaceOnly})},
		DocChanges: map[string][]detect.LineRange{"README.md": {{Start: 8, Lines: 1}, {Start: 4, Lines: 0}}},
		Commits:    map[string]detect.FileCommits{"a.go": {Commits: []detect.CommitRef{{Hash: "abc", Time: 5, Subject: "rename"}}, Truncated: true}},
	}
	evals := detect.Evaluate(in)

	usage := evalOf(t, evals, "README.md#usage")
	if usage.Mode != detect.ModeDiff || usage.Base != "main" || usage.Heading != "Usage" || usage.Line != 3 {
		t.Fatalf("header = %+v", usage)
	}
	if usage.OwnStart != 3 || usage.OwnEnd != 6 {
		t.Fatalf("own range = %d-%d, want 3-6", usage.OwnStart, usage.OwnEnd)
	}
	if !reflect.DeepEqual(usage.IgnoreRules, []string{"vendor/**"}) {
		t.Fatalf("ignore rules = %v", usage.IgnoreRules)
	}
	if usage.Verdict != detect.VerdictNoDrift || usage.Reason != detect.ReasonOwnLinesChanged {
		t.Fatalf("verdict = %s/%s", usage.Verdict, usage.Reason)
	}
	// The pure deletion after line 4 touches the own range, the edit at line 8 (child) does not.
	if !usage.DocChanged || !reflect.DeepEqual(usage.OwnHunks, []detect.LineRange{{Start: 4, Lines: 0}}) {
		t.Fatalf("own hunks = %+v (docChanged %v)", usage.OwnHunks, usage.DocChanged)
	}
	if len(usage.Files) != 1 {
		t.Fatalf("files = %+v", usage.Files)
	}
	file := usage.Files[0]
	if file.Path != "a.go" || !file.Triggers || len(file.Changes) != 1 || file.Changes[0].Status != "R" || file.Changes[0].Path != "new/a.go" {
		t.Fatalf("file = %+v", file)
	}
	if !file.CommitsTruncated || len(file.Commits) != 1 || file.Commits[0].Subject != "rename" {
		t.Fatalf("commits = %+v", file)
	}

	flags := evalOf(t, evals, "README.md#flags")
	if flags.Verdict != detect.VerdictNoDrift || flags.Reason != detect.ReasonAllIgnored {
		t.Fatalf("flags verdict = %s/%s", flags.Verdict, flags.Reason)
	}
	if flags.Files[0].Triggers || flags.Files[0].Changes[0].Decision.Reason != relevance.ReasonWhitespaceOnly {
		t.Fatalf("flags file = %+v", flags.Files[0])
	}
}

func TestEvaluateRecordsLinkOriginsAndUnresolvedReferences(t *testing.T) {
	d := []links.DocLinks{{Doc: "README.md", Sections: []links.SectionLinks{{
		Heading: "Usage", Anchor: "usage", Level: 2, StartLine: 3, EndLine: 9,
		Links:   []links.Link{{File: "a.go", Origin: links.OriginAnnotation}, {File: "b.go", Origin: links.OriginConfig}},
		Missing: []links.Missing{{Ref: "gone/**", Origin: links.OriginAnnotation}},
	}, {
		Heading: "Bare", Anchor: "bare", Level: 2, StartLine: 10, EndLine: 12, Unmapped: true, Links: []links.Link{}, Missing: []links.Missing{},
	}}}}
	evals := detect.Evaluate(detect.EvalInput{Mode: detect.ModeDiff, Docs: d})
	usage := evalOf(t, evals, "README.md#usage")
	if usage.Files[0].Origin != links.OriginAnnotation || usage.Files[1].Origin != links.OriginConfig {
		t.Fatalf("origins = %+v", usage.Files)
	}
	if len(usage.Missing) != 1 || usage.Missing[0].Ref != "gone/**" || usage.Unmapped {
		t.Fatalf("missing = %+v unmapped=%v", usage.Missing, usage.Unmapped)
	}
	if bare := evalOf(t, evals, "README.md#bare"); !bare.Unmapped || bare.Reason != detect.ReasonNoMappedCode {
		t.Fatalf("bare = %+v", bare)
	}
}

func TestEvaluateHistory(t *testing.T) {
	section := func(time int64) map[detect.SectionKey]detect.CommitRef {
		return map[detect.SectionKey]detect.CommitRef{usageKey: {Hash: "d0c", Time: time}}
	}
	files := func(time int64) map[string]detect.HistoryFile {
		return map[string]detect.HistoryFile{"a.go": {Newest: &detect.CommitRef{Hash: "c0de", Time: time}}}
	}
	tests := []struct {
		name         string
		sectionDates map[detect.SectionKey]detect.CommitRef
		files        map[string]detect.HistoryFile
		lock         map[detect.SectionKey][]string
		wantVerdict  detect.Verdict
		wantReason   detect.Reason
		wantEffect   detect.LockEffect
		wantTrigger  bool
	}{
		{name: "code newer", sectionDates: section(10), files: files(20), wantVerdict: detect.VerdictDrift, wantReason: detect.ReasonCodeNewer, wantEffect: detect.LockEffectNone, wantTrigger: true},
		{name: "doc newer", sectionDates: section(30), files: files(20), wantVerdict: detect.VerdictNoDrift, wantReason: detect.ReasonCodeNotNewer, wantEffect: detect.LockEffectNone},
		{name: "equal dates", sectionDates: section(20), files: files(20), wantVerdict: detect.VerdictNoDrift, wantReason: detect.ReasonCodeNotNewer, wantEffect: detect.LockEffectNone},
		{
			name: "only ignored code", sectionDates: nil,
			files:       map[string]detect.HistoryFile{"a.go": {IgnoredGlob: "**/*.go"}},
			wantVerdict: detect.VerdictNoDrift, wantReason: detect.ReasonNoDatedCode, wantEffect: detect.LockEffectNone,
		},
		{name: "no blame data", files: files(20), wantVerdict: detect.VerdictNoDrift, wantReason: detect.ReasonNoBlameData, wantEffect: detect.LockEffectNone},
		{
			name: "matching entry suppresses dated drift", sectionDates: section(10), files: files(20),
			lock:        map[detect.SectionKey][]string{usageKey: {}},
			wantVerdict: detect.VerdictNoDrift, wantReason: detect.ReasonAcknowledged, wantEffect: detect.LockEffectSuppressed, wantTrigger: true,
		},
		{
			name: "mismatching entry forces drift despite newer docs", sectionDates: section(30), files: files(20),
			lock:        map[detect.SectionKey][]string{usageKey: {"a.go"}},
			wantVerdict: detect.VerdictDrift, wantReason: detect.ReasonLockMismatch, wantEffect: detect.LockEffectForcedDrift,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evals := detect.Evaluate(detect.EvalInput{Mode: detect.ModeHistory, Docs: evalDocs(), SectionDates: tt.sectionDates, HistoryFiles: tt.files, Lock: tt.lock})
			e := evalOf(t, evals, "README.md#usage")
			if e.Verdict != tt.wantVerdict || e.Reason != tt.wantReason || e.Lock.Effect != tt.wantEffect {
				t.Fatalf("verdict/reason/effect = %s/%s/%s, want %s/%s/%s", e.Verdict, e.Reason, e.Lock.Effect, tt.wantVerdict, tt.wantReason, tt.wantEffect)
			}
			if got := e.Files[0].Triggers; got != tt.wantTrigger {
				t.Fatalf("file triggers = %v, want %v", got, tt.wantTrigger)
			}
		})
	}
}

// The check pipeline reports exactly the evaluations that ended as drift, in
// the order the standalone decision functions produce them.
func TestDriftsMatchTheDecisionFunctions(t *testing.T) {
	changes := []detect.FileChange{kept("a.go"), kept("b.go"), kept("c.go")}
	lock := map[detect.SectionKey][]string{flagsKey: {}}
	docChanges := map[string][]detect.LineRange{"README.md": {{Start: 12, Lines: 1}}}

	got := detect.Evaluate(detect.EvalInput{Mode: detect.ModeDiff, Docs: evalDocs(), Changes: changes, DocChanges: docChanges, Lock: lock}).Drifts()

	plain := []detect.Change{{Path: "a.go"}, {Path: "b.go"}, {Path: "c.go"}}
	want := detect.ApplyLock(evalDocs(), detect.Detect(detect.Input{Docs: evalDocs(), Changes: plain, DocChanges: docChanges}), lock, false)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("drifts = %+v\nwant %+v", got, want)
	}
	if len(got) != 1 || got[0].Section != "Usage" {
		t.Fatalf("drifts = %+v, want only Usage", got)
	}
}

func TestHistoryDriftsMatchTheDecisionFunctions(t *testing.T) {
	sectionDates := map[detect.SectionKey]detect.CommitRef{usageKey: {Time: 10}, flagsKey: {Time: 50}}
	historyFiles := map[string]detect.HistoryFile{
		"a.go": {Newest: &detect.CommitRef{Time: 20}},
		"b.go": {Newest: &detect.CommitRef{Time: 20}},
	}
	lock := map[detect.SectionKey][]string{{Doc: "README.md", Line: 11}: {"c.go"}}

	got := detect.Evaluate(detect.EvalInput{Mode: detect.ModeHistory, Docs: evalDocs(), SectionDates: sectionDates, HistoryFiles: historyFiles, Lock: lock}).Drifts()

	want := detect.ApplyLock(evalDocs(), detect.History(detect.HistoryInput{
		Docs:         evalDocs(),
		SectionDates: map[detect.SectionKey]int64{usageKey: 10, flagsKey: 50},
		FileDates:    map[string]int64{"a.go": 20, "b.go": 20},
	}), lock, true)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("drifts = %+v\nwant %+v", got, want)
	}
}

func TestWithSeverityMergesClassification(t *testing.T) {
	evals := detect.Evaluate(detect.EvalInput{Mode: detect.ModeDiff, Docs: evalDocs(), Changes: []detect.FileChange{kept("a.go"), kept("b.go")}})
	refs := map[detect.SectionKey][]docparse.Reference{usageKey: {{Kind: docparse.KindFlag, Value: "--gone", Line: 4}}}
	versions := map[string]detect.FileVersion{"a.go": {Old: []byte("x --gone y"), New: []byte("x y")}, "b.go": {Old: []byte{}, New: []byte{}}}

	classified := evals.WithSeverity(evalDocs(), refs, versions)

	usage := evalOf(t, classified, "README.md#usage")
	if usage.Drift.Severity != "high" || len(usage.Drift.BrokenReferences) != 1 {
		t.Fatalf("usage = %s %+v", usage.Drift.Severity, usage.Drift.BrokenReferences)
	}
	if flags := evalOf(t, classified, "README.md#flags"); flags.Drift.Severity != "low" {
		t.Fatalf("flags severity = %s", flags.Drift.Severity)
	}
	if want := detect.Classify(evalDocs(), refs, versions, evals.Drifts()); !reflect.DeepEqual(classified.Drifts(), want) {
		t.Fatalf("drifts = %+v, want %+v", classified.Drifts(), want)
	}
	// The input is not modified.
	if evalOf(t, evals, "README.md#usage").Drift.Severity != "low" {
		t.Fatal("WithSeverity mutated its receiver")
	}
}

// evalDocs is docs() with anchors, as links.Resolve produces them.
func evalDocs() []links.DocLinks {
	d := docs()
	for i, s := range d[0].Sections {
		d[0].Sections[i].Anchor = anchorAt(s.StartLine)
	}
	return d
}
