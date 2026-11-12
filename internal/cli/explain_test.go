package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/cli"
	"github.com/JtheGunner/doc-drift-checker/internal/config"
	"github.com/JtheGunner/doc-drift-checker/internal/relevance"
	"github.com/JtheGunner/doc-drift-checker/internal/testutil"
)

// explainCommit is a commit of the explain JSON.
type explainCommit struct {
	Hash    string `json:"hash"`
	Date    string `json:"date"`
	Subject string `json:"subject"`
}

type explainFile struct {
	Path     string `json:"path"`
	Origin   string `json:"origin"`
	Triggers bool   `json:"triggers"`
	Changes  []struct {
		Status    string `json:"status"`
		Path      string `json:"path"`
		OldPath   string `json:"oldPath"`
		Relevance struct {
			Relevant bool   `json:"relevant"`
			Reason   string `json:"reason"`
			Glob     string `json:"glob"`
			OldGlob  string `json:"oldGlob"`
			Analyzer string `json:"analyzer"`
		} `json:"relevance"`
	} `json:"changes"`
	Commits          []explainCommit `json:"commits"`
	CommitsTruncated bool            `json:"commitsTruncated"`
	IgnoredGlob      string          `json:"ignoredGlob"`
	NewestCommit     *explainCommit  `json:"newestCommit"`
}

type explainReport struct {
	SchemaVersion int    `json:"schemaVersion"`
	Mode          string `json:"mode"`
	Base          string `json:"base"`
	Section       struct {
		Key     string `json:"key"`
		Doc     string `json:"doc"`
		Heading string `json:"heading"`
		Line    int    `json:"line"`
	} `json:"section"`
	Verdict         string   `json:"verdict"`
	Reason          string   `json:"reason"`
	Severity        string   `json:"severity"`
	TriggeringFiles []string `json:"triggeringFiles"`
	Links           []struct {
		File   string `json:"file"`
		Origin string `json:"origin"`
	} `json:"links"`
	Unresolved []struct {
		Ref    string `json:"ref"`
		Origin string `json:"origin"`
		Line   int    `json:"line"`
	} `json:"unresolved"`
	Unmapped    bool          `json:"unmapped"`
	IgnoreRules []string      `json:"ignoreRules"`
	Files       []explainFile `json:"files"`
	Own         struct {
		DocChanged bool `json:"docChanged"`
		Hunks      []struct {
			Start int `json:"start"`
			Lines int `json:"lines"`
		} `json:"hunks"`
	} `json:"own"`
	SectionDate *explainCommit `json:"sectionDate"`
	Lock        struct {
		State     string   `json:"state"`
		Differing []string `json:"differing"`
		Effect    string   `json:"effect"`
	} `json:"lock"`
}

func (r explainReport) file(t *testing.T, path string) explainFile {
	t.Helper()
	for _, f := range r.Files {
		if f.Path == path {
			return f
		}
	}
	t.Fatalf("explanation has no file %q: %+v", path, r.Files)
	return explainFile{}
}

// runExplain runs the explain command and returns stdout, stderr and the exit code.
func runExplain(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd := cli.NewRootCmd("dev")
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(append([]string{"explain"}, args...))
	err := cmd.Execute()
	return out.String(), errOut.String(), cli.ExitCode(err)
}

// explainJSON explains a section in JSON and asserts exit code 0. Pass "--base",
// "main" in args for diff mode.
func explainJSON(t *testing.T, section string, args ...string) explainReport {
	t.Helper()
	out, errOut, code := runExplain(t, append([]string{section, "--format", "json"}, args...)...)
	if code != 0 {
		t.Fatalf("explain %s %v: exit code %d\nstdout: %s\nstderr: %s", section, args, code, out, errOut)
	}
	var rep explainReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if rep.SchemaVersion != 1 {
		t.Fatalf("schemaVersion = %d", rep.SchemaVersion)
	}
	return rep
}

func explainText(t *testing.T, section string, args ...string) string {
	t.Helper()
	out, errOut, code := runExplain(t, append([]string{section}, args...)...)
	if code != 0 {
		t.Fatalf("explain %s %v: exit code %d\nstdout: %s\nstderr: %s", section, args, code, out, errOut)
	}
	return out
}

// explainFails runs explain expecting exit code 2 and returns the error text.
func explainFails(t *testing.T, args ...string) string {
	t.Helper()
	cmd := cli.NewRootCmd("dev")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(append([]string{"explain"}, args...))
	err := cmd.Execute()
	if cli.ExitCode(err) != cli.ExitFailure || err == nil {
		t.Fatalf("explain %v: err = %v, want exit code %d\nstdout: %s", args, err, cli.ExitFailure, out.String())
	}
	return err.Error()
}

// sectionKeys lists the key of every anchored section of the configured docs.
func sectionKeys(t *testing.T) []string {
	t.Helper()
	var out bytes.Buffer
	cmd := cli.NewRootCmd("dev")
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"links", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var rep struct {
		Docs []struct {
			Doc      string `json:"doc"`
			Sections []struct {
				Anchor string `json:"anchor"`
			} `json:"sections"`
		} `json:"docs"`
	}
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, d := range rep.Docs {
		for _, s := range d.Sections {
			if s.Anchor != "" {
				keys = append(keys, d.Doc+"#"+s.Anchor)
			}
		}
	}
	return keys
}

// checkAll runs check in the given mode and returns its drifts by "<doc>:<line>".
func checkAll(t *testing.T, diff bool) map[string]struct {
	Files    []string
	Severity string
} {
	t.Helper()
	args := []string{"--format", "json"}
	if diff {
		args = append(args, "--base", "main")
	}
	out, errOut, code := runCheck(t, args...)
	if code != 0 && code != cli.ExitDrift {
		t.Fatalf("check: exit code %d\n%s\n%s", code, out, errOut)
	}
	var rep struct {
		Drifts []struct {
			Doc      string   `json:"doc"`
			Line     int      `json:"line"`
			Files    []string `json:"files"`
			Severity string   `json:"severity"`
		} `json:"drifts"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatal(err)
	}
	drifts := map[string]struct {
		Files    []string
		Severity string
	}{}
	for _, d := range rep.Drifts {
		drifts[fmt.Sprintf("%s:%d", d.Doc, d.Line)] = struct {
			Files    []string
			Severity string
		}{d.Files, d.Severity}
	}
	return drifts
}

// assertExplainAgreesWithCheck is the "no second code path" test: for every
// section, explain's verdict equals whether check reported it, and a drifted
// section lists the same triggering files and severity.
func assertExplainAgreesWithCheck(t *testing.T, diff bool) {
	t.Helper()
	drifts := checkAll(t, diff)
	args := []string{}
	if diff {
		args = append(args, "--base", "main")
	}
	keys := sectionKeys(t)
	if len(keys) == 0 {
		t.Fatal("no sections")
	}
	seen := 0
	for _, key := range keys {
		rep := explainJSON(t, key, args...)
		d, reported := drifts[fmt.Sprintf("%s:%d", rep.Section.Doc, rep.Section.Line)]
		if reported {
			seen++
		}
		if got := rep.Verdict == "drift"; got != reported {
			t.Errorf("%s: explain verdict %q (reason %s), check reported = %v", key, rep.Verdict, rep.Reason, reported)
			continue
		}
		if !reported {
			if len(rep.TriggeringFiles) != 0 || rep.Severity != "" {
				t.Errorf("%s: no drift but triggering files %v severity %q", key, rep.TriggeringFiles, rep.Severity)
			}
			continue
		}
		if !reflect.DeepEqual(rep.TriggeringFiles, d.Files) {
			t.Errorf("%s: triggering files %v, check %v", key, rep.TriggeringFiles, d.Files)
		}
		if rep.Severity != d.Severity {
			t.Errorf("%s: severity %q, check %q", key, rep.Severity, d.Severity)
		}
	}
	if seen != len(drifts) {
		t.Errorf("explain covered %d of %d drifts of check", seen, len(drifts))
	}
}

const explainSeverityReadme = "# Project\n\n## Flags\n\nUse `--verbose`, see `src/flags.go`.\n"

func TestExplainAgreesWithCheck(t *testing.T) {
	scenarios := []struct {
		name  string
		diff  bool
		setup func(t *testing.T)
	}{
		{"diff: code only changed", true, func(t *testing.T) {
			checkRepo(t).CommitFile("src/lib.go", "package src\n\nfunc New() {}\n", "change lib")
		}},
		{"diff: code and docs changed", true, func(t *testing.T) {
			r := checkRepo(t)
			r.Write("src/lib.go", "package src\n\nfunc New() {}\n")
			r.Write("README.md", editUsage(checkReadme))
			r.Commit("both")
		}},
		{"diff: docs only changed", true, func(t *testing.T) {
			checkRepo(t).CommitFile("README.md", editUsage(checkReadme), "docs")
		}},
		{"diff: rename", true, func(t *testing.T) {
			r := checkRepo(t)
			r.Move("src/lib.go", "src/library.go")
			r.Commit("rename")
		}},
		{"diff: delete", true, func(t *testing.T) {
			r := checkRepo(t)
			r.Remove("src/lib.go")
			r.Commit("delete")
		}},
		{"diff: ignored file only", true, func(t *testing.T) {
			relevanceRepo(t).CommitFile("src/lib_test.go", "changed\n", "test change")
		}},
		{"diff: whitespace only", true, func(t *testing.T) {
			relevanceRepo(t).CommitFile("src/lib.go", "package src\n\n\nfunc A() {\n    x := 1   \n}\n", "reformat")
		}},
		{"diff: Go API unchanged", true, func(t *testing.T) {
			symbolsRepo(t).CommitFile("src/lib.go", strings.Replace(libBase, "fmt.Sprint(name)", "fmt.Sprint(name, name)", 1), "body")
		}},
		{"diff: Go API changed", true, func(t *testing.T) {
			symbolsRepo(t).CommitFile("src/lib.go", libBase+"\nfunc Stop() {}\n", "API")
		}},
		{"diff: high severity", true, func(t *testing.T) {
			r := testutil.NewRepo(t)
			r.Write("README.md", explainSeverityReadme)
			r.Write("src/flags.go", "package src\n\nvar f = \"verbose\"\n")
			r.Commit("base")
			r.Branch("feature")
			t.Chdir(r.Dir)
			r.CommitFile("src/flags.go", "package src\n\nvar f = \"quiet\"\n", "rename flag")
		}},
		{"diff: lock suppresses", true, func(t *testing.T) {
			r := checkRepo(t)
			r.CommitFile("src/lib.go", "package src\n\nfunc New() {}\n", "change lib")
			r.CommitFile("other/o.go", "package other\n\nfunc O() {}\n", "change other")
			mustAck(t, "usage")
		}},
		{"diff: lock mismatch", true, func(t *testing.T) {
			r := checkRepo(t)
			mustAck(t, "usage")
			r.CommitFile("src/lib.go", changedLib, "change lib")
		}},
		{"history: code newer", false, func(t *testing.T) {
			historyRepo(t).CommitFile("src/lib.go", newLib, "change code")
		}},
		{"history: doc newer", false, func(t *testing.T) {
			r := historyRepo(t)
			r.CommitFile("src/lib.go", newLib, "change code")
			r.CommitFile("README.md", editUsage(checkReadme), "docs updated")
		}},
		{"history: equal dates", false, func(t *testing.T) {
			historyRepo(t)
		}},
		{"history: ignored file only", false, func(t *testing.T) {
			r := historyRepo(t)
			r.CommitFile("other/o_test.go", "package other\n", "add test")
		}},
		{"history: lock suppresses newer code", false, func(t *testing.T) {
			r := historyRepo(t)
			r.CommitFile("src/lib.go", newLib, "code newer than docs")
			mustAck(t, "usage")
			r.Commit("record lockfile")
		}},
		{"history: lock mismatch wins over newer docs", false, func(t *testing.T) {
			r := historyRepo(t)
			mustAck(t, "usage")
			r.CommitFile("src/lib.go", newLib, "change code")
			r.CommitFile("README.md", editUsage(checkReadme), "docs updated after the code")
		}},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			sc.setup(t)
			assertExplainAgreesWithCheck(t, sc.diff)
		})
	}
}

func TestExplainDiffCodeChangedListsCommitsAndRelevance(t *testing.T) {
	r := symbolsRepo(t)
	r.CommitFile("src/lib.go", strings.Replace(libBase, "Run(name string)", "Run(name string, n int)", 1), "change the signature")
	head := r.Git("rev-parse", "HEAD")

	rep := explainJSON(t, "README.md#library", "--base", "main")
	if rep.Mode != "diff" || rep.Base != "main" || rep.Verdict != "drift" || rep.Reason != "code-changed" || rep.Severity != "low" {
		t.Fatalf("report = %+v", rep)
	}
	if !reflect.DeepEqual(rep.TriggeringFiles, []string{"src/lib.go"}) {
		t.Fatalf("triggering files = %v", rep.TriggeringFiles)
	}
	file := rep.file(t, "src/lib.go")
	if file.Origin != "implicit" || !file.Triggers || len(file.Changes) != 1 {
		t.Fatalf("file = %+v", file)
	}
	change := file.Changes[0]
	if change.Status != "M" || !change.Relevance.Relevant || change.Relevance.Reason != "api-changed" || change.Relevance.Analyzer != ".go" {
		t.Fatalf("change = %+v", change)
	}
	if len(file.Commits) != 1 || file.Commits[0].Hash != head || file.Commits[0].Subject != "change the signature" || file.CommitsTruncated {
		t.Fatalf("commits = %+v", file.Commits)
	}
	if rep.Own.DocChanged || len(rep.Own.Hunks) != 0 {
		t.Fatalf("own = %+v", rep.Own)
	}
	if rep.Lock.State != "none" || rep.Lock.Effect != "none" {
		t.Fatalf("lock = %+v", rep.Lock)
	}
}

func TestExplainDiffNoDriftReasons(t *testing.T) {
	tests := []struct {
		name       string
		repo       func(t *testing.T) *testutil.Repo
		change     func(r *testutil.Repo)
		section    string
		wantReason string
	}{
		{"no mapped file changed", checkRepo, func(r *testutil.Repo) { r.CommitFile("src/flags.go", newFlags, "other code") }, "README.md#usage", "no-mapped-file-changed"},
		{"section has no mapped code", checkRepo, func(r *testutil.Repo) { r.CommitFile("src/lib.go", newLib, "change") }, "README.md#project", "no-mapped-code"},
		{"own lines changed", checkRepo, func(r *testutil.Repo) {
			r.Write("src/lib.go", newLib)
			r.Write("README.md", editUsage(checkReadme))
			r.Commit("both")
		}, "README.md#usage", "own-lines-changed"},
		{"all changes ignored", relevanceRepo2, func(r *testutil.Repo) { r.CommitFile("src/lib_test.go", "changed\n", "test") }, "README.md#quality", "all-changes-ignored"},
		{"acknowledged", checkRepo, func(r *testutil.Repo) {
			r.CommitFile("src/lib.go", newLib, "change")
			mustAck(t, "usage")
		}, "README.md#usage", "acknowledged"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := tt.repo(t)
			tt.change(r)
			rep := explainJSON(t, tt.section, "--base", "main")
			if rep.Verdict != "no-drift" || rep.Reason != tt.wantReason {
				t.Fatalf("verdict/reason = %s/%s, want no-drift/%s", rep.Verdict, rep.Reason, tt.wantReason)
			}
		})
	}
}

func relevanceRepo2(t *testing.T) *testutil.Repo { return relevanceRepo(t) }

func TestExplainOwnLinesChangedNamesTheHunks(t *testing.T) {
	r := checkRepo(t)
	r.Write("src/lib.go", newLib)
	r.Write("README.md", editUsage(checkReadme))
	r.Commit("both")

	rep := explainJSON(t, "usage", "--base", "main")
	if !rep.Own.DocChanged || len(rep.Own.Hunks) != 1 || rep.Own.Hunks[0].Start != 5 {
		t.Fatalf("own = %+v", rep.Own)
	}
	text := explainText(t, "usage", "--base", "main")
	if !strings.Contains(text, "the section's own lines changed") || !strings.Contains(text, "lines 5-5") {
		t.Fatalf("text:\n%s", text)
	}
}

func TestExplainNamesTheIgnoreGlobAndLockEffect(t *testing.T) {
	t.Run("default glob", func(t *testing.T) {
		r := relevanceRepo(t)
		r.CommitFile("src/lib_test.go", "changed\n", "test")
		want, _ := relevance.NewFilter(config.DefaultIgnore()).MatchingGlob("src/lib_test.go")

		rep := explainJSON(t, "quality", "--base", "main")
		change := rep.file(t, "src/lib_test.go").Changes[0]
		if change.Relevance.Relevant || change.Relevance.Reason != "ignored-path" || change.Relevance.Glob != want || want == "" {
			t.Fatalf("change = %+v, want glob %q", change, want)
		}
		if !reflect.DeepEqual(rep.IgnoreRules, config.DefaultIgnore()) {
			t.Fatalf("ignore rules = %v", rep.IgnoreRules)
		}
		text := explainText(t, "quality", "--base", "main")
		if !strings.Contains(text, fmt.Sprintf("dropped, matches ignore glob %q", want)) {
			t.Fatalf("text:\n%s", text)
		}
	})
	t.Run("configured glob", func(t *testing.T) {
		r := relevanceRepo(t, ".docdrift.yaml", "ignore: [\"src/lib.go\"]\n")
		r.CommitFile("src/lib.go", "package src\n\nfunc B() {}\n", "change ignored by config")
		rep := explainJSON(t, "usage", "--base", "main")
		change := rep.file(t, "src/lib.go").Changes[0]
		if change.Relevance.Reason != "ignored-path" || change.Relevance.Glob != "src/lib.go" || rep.Reason != "all-changes-ignored" {
			t.Fatalf("change = %+v, reason %s", change, rep.Reason)
		}
	})
	t.Run("whitespace only", func(t *testing.T) {
		relevanceRepo(t).CommitFile("src/lib.go", "package src\n\n\nfunc A() {\n    x := 1   \n}\n", "reformat")
		rep := explainJSON(t, "usage", "--base", "main")
		if got := rep.file(t, "src/lib.go").Changes[0].Relevance.Reason; got != "whitespace-only" || rep.Reason != "all-changes-ignored" {
			t.Fatalf("reason %q / %s", got, rep.Reason)
		}
	})
	t.Run("Go API unchanged", func(t *testing.T) {
		symbolsRepo(t).CommitFile("src/lib.go", strings.Replace(libBase, "return 1", "return 2", 1), "private change")
		rep := explainJSON(t, "library", "--base", "main")
		rel := rep.file(t, "src/lib.go").Changes[0].Relevance
		if rel.Relevant || rel.Reason != "api-unchanged" || rel.Analyzer != ".go" {
			t.Fatalf("relevance = %+v", rel)
		}
		if text := explainText(t, "library", "--base", "main"); !strings.Contains(text, "dropped, exported API unchanged (analyzer .go)") {
			t.Fatalf("text:\n%s", text)
		}
	})
	t.Run("no analyzer", func(t *testing.T) {
		symbolsRepo(t).CommitFile("scripts/tool.py", "def run():\n    return 2\n", "python")
		rep := explainJSON(t, "tool", "--base", "main")
		rel := rep.file(t, "scripts/tool.py").Changes[0].Relevance
		if !rel.Relevant || rel.Reason != "no-analyzer" || rep.Verdict != "drift" {
			t.Fatalf("relevance = %+v, verdict %s", rel, rep.Verdict)
		}
	})
	t.Run("rename across the ignore boundary", func(t *testing.T) {
		r := relevanceRepo(t)
		r.Move("src/lib.go", "src/lib_gen.go")
		r.Commit("rename into a generated name")
		rep := explainJSON(t, "usage", "--base", "main")
		change := rep.file(t, "src/lib.go").Changes[0]
		if change.Status != "R" || change.OldPath != "src/lib.go" || change.Relevance.Reason != "rename-across-ignore" || !change.Relevance.Relevant {
			t.Fatalf("change = %+v", change)
		}
		if !reflect.DeepEqual(rep.TriggeringFiles, []string{"src/lib.go -> src/lib_gen.go"}) {
			t.Fatalf("triggering files = %v", rep.TriggeringFiles)
		}
	})
}

func TestExplainLockStates(t *testing.T) {
	t.Run("diff: matching entry suppresses", func(t *testing.T) {
		r := checkRepo(t)
		r.CommitFile("src/lib.go", newLib, "change")
		mustAck(t, "usage")
		rep := explainJSON(t, "usage", "--base", "main")
		if rep.Lock.State != "matches" || rep.Lock.Effect != "suppressed" || rep.Verdict != "no-drift" {
			t.Fatalf("report = %+v", rep)
		}
		if text := explainText(t, "usage", "--base", "main"); !strings.Contains(text, "the drift is suppressed") {
			t.Fatalf("text:\n%s", text)
		}
	})
	t.Run("diff: mismatching entry has no effect", func(t *testing.T) {
		r := checkRepo(t)
		mustAck(t, "usage")
		r.CommitFile("src/lib.go", changedLib, "change")
		rep := explainJSON(t, "usage", "--base", "main")
		if rep.Lock.State != "mismatch" || rep.Lock.Effect != "no-effect" || rep.Verdict != "drift" || !reflect.DeepEqual(rep.Lock.Differing, []string{"src/lib.go"}) {
			t.Fatalf("lock = %+v verdict %s", rep.Lock, rep.Verdict)
		}
	})
	t.Run("history: mismatch forces drift", func(t *testing.T) {
		r := historyRepo(t)
		mustAck(t, "usage")
		r.CommitFile("src/lib.go", newLib, "change code")
		r.CommitFile("README.md", editUsage(checkReadme), "docs updated after the code")
		rep := explainJSON(t, "usage")
		if rep.Verdict != "drift" || rep.Reason != "lock-mismatch" || rep.Lock.Effect != "forced-drift" || !reflect.DeepEqual(rep.TriggeringFiles, []string{"src/lib.go"}) {
			t.Fatalf("report = %+v", rep)
		}
	})
}

func TestExplainHistoryDatesCodeAndSection(t *testing.T) {
	r := historyRepo(t)
	base := r.Git("rev-parse", "HEAD")
	code := r.CommitFile("src/lib.go", newLib, "newer code")

	rep := explainJSON(t, "usage")
	if rep.Mode != "history" || rep.Base != "" || rep.Verdict != "drift" || rep.Reason != "code-newer" {
		t.Fatalf("report = %+v", rep)
	}
	file := rep.file(t, "src/lib.go")
	if file.NewestCommit == nil || file.NewestCommit.Hash != code || file.NewestCommit.Subject != "newer code" || !file.Triggers {
		t.Fatalf("file = %+v", file)
	}
	if rep.SectionDate == nil || rep.SectionDate.Hash != base {
		t.Fatalf("section date = %+v, want the base commit %s", rep.SectionDate, base)
	}

	doc := explainJSON(t, "flags")
	if doc.Verdict != "no-drift" || doc.Reason != "code-not-newer" {
		t.Fatalf("flags = %s/%s", doc.Verdict, doc.Reason)
	}
}

func TestExplainHistoryIgnoredFileAndIgnoreRevs(t *testing.T) {
	r := historyRepo(t)
	r.CommitFile("other/o_test.go", "package other\n", "add test")
	rep := explainJSON(t, "other")
	file := rep.file(t, "other/o_test.go")
	if file.IgnoredGlob != "**/*_test.go" || file.NewestCommit != nil || rep.Verdict != "no-drift" {
		t.Fatalf("file = %+v verdict %s", file, rep.Verdict)
	}

	format := r.CommitFile("src/lib.go", "package src\n\n\n", "format code")
	if got := explainJSON(t, "usage"); got.Verdict != "drift" {
		t.Fatalf("without the revs file: %s", got.Verdict)
	}
	revs := writeRevs(t, format)
	if got := explainJSON(t, "usage", "--ignore-revs-file", revs); got.Verdict != "no-drift" {
		t.Fatalf("with the revs file: %s/%s", got.Verdict, got.Reason)
	}
}

func TestExplainLinkOriginsAndUnresolvedReferences(t *testing.T) {
	r := testutil.NewRepo(t)
	r.Write("README.md", "# Project\n\n"+
		"## Annotated\n\n<!-- drift: src/a.go, gone/** -->\n\nText.\n\n"+
		"## Configured\n\nText.\n\n"+
		"## Implicit\n\nSee `src/c.go` and `src/missing.go`.\n\n"+
		"## Bare\n\nNothing to see.\n")
	r.Write(".docdrift.yaml", "mappings:\n  Configured:\n    - src/b.go\n")
	r.Write("src/a.go", "package src\n")
	r.Write("src/b.go", "package src\n")
	r.Write("src/c.go", "package src\n")
	r.Commit("base")
	t.Chdir(r.Dir)

	annotated := explainJSON(t, "annotated")
	if len(annotated.Links) != 1 || annotated.Links[0].File != "src/a.go" || annotated.Links[0].Origin != "annotation" {
		t.Fatalf("annotated links = %+v", annotated.Links)
	}
	if len(annotated.Unresolved) != 1 || annotated.Unresolved[0].Ref != "gone/**" || annotated.Unresolved[0].Origin != "annotation" {
		t.Fatalf("annotated unresolved = %+v", annotated.Unresolved)
	}
	if configured := explainJSON(t, "configured"); len(configured.Links) != 1 || configured.Links[0].Origin != "config" {
		t.Fatalf("configured links = %+v", configured.Links)
	}
	implicit := explainJSON(t, "implicit")
	if len(implicit.Links) != 1 || implicit.Links[0].Origin != "implicit" || len(implicit.Unresolved) != 1 || implicit.Unresolved[0].Ref != "src/missing.go" || implicit.Unresolved[0].Line == 0 {
		t.Fatalf("implicit = %+v / %+v", implicit.Links, implicit.Unresolved)
	}
	bare := explainJSON(t, "bare")
	if !bare.Unmapped || bare.Reason != "no-mapped-code" || len(bare.Links) != 0 {
		t.Fatalf("bare = %+v", bare)
	}
	text := explainText(t, "implicit")
	for _, want := range []string{"src/c.go (implicit)", "src/missing.go (implicit, line 15)"} {
		if !strings.Contains(text, want) {
			t.Errorf("text does not contain %q:\n%s", want, text)
		}
	}
	if text := explainText(t, "bare"); !strings.Contains(text, "The section has no links and no references to files.") {
		t.Errorf("text:\n%s", text)
	}
}

func TestExplainTruncatesLongCommitLists(t *testing.T) {
	r := checkRepo(t)
	for i := 0; i < 22; i++ {
		r.CommitFile("src/lib.go", fmt.Sprintf("package src\n\nvar V%d = %d\n", i, i), fmt.Sprintf("edit %d", i))
	}
	rep := explainJSON(t, "usage", "--base", "main")
	file := rep.file(t, "src/lib.go")
	if len(file.Commits) != 20 || !file.CommitsTruncated {
		t.Fatalf("commits = %d truncated = %v", len(file.Commits), file.CommitsTruncated)
	}
	if file.Commits[0].Subject != "edit 21" {
		t.Fatalf("newest first expected, got %q", file.Commits[0].Subject)
	}
	if text := explainText(t, "usage", "--base", "main"); !strings.Contains(text, "list truncated") {
		t.Fatalf("text lacks the truncation note:\n%s", text)
	}
}

func TestExplainNeutralisesControlCharactersInCommitSubjects(t *testing.T) {
	r := checkRepo(t)
	r.CommitFile("src/lib.go", newLib, "evil \x1b[31mred\x1b[0m subject")

	text := explainText(t, "usage", "--base", "main")
	if strings.ContainsRune(text, 0x1b) {
		t.Fatalf("raw escape in the text output:\n%q", text)
	}
	if !strings.Contains(text, `evil \x1b[31mred\x1b[0m subject`) {
		t.Fatalf("escaped subject missing:\n%s", text)
	}
}

func TestExplainTextShowsVerdictLinksAndFiles(t *testing.T) {
	r := checkRepo(t)
	r.CommitFile("src/lib.go", newLib, "change lib")
	text := explainText(t, "README.md#usage", "--base", "main")
	for _, want := range []string{
		"Section:  Usage", "Key:      README.md#usage", "Mode:     diff (base main)",
		"Verdict:  drift (severity low)", "Triggering files:\n  src/lib.go",
		"src/lib.go (implicit)", "status: modified", "commits in main..HEAD", "change lib",
		"Lockfile doc-drift.lock:\n  no entry for this section",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text does not contain %q:\n%s", want, text)
		}
	}
	if code := mustExplainCode(t, "README.md#usage", "--base", "main"); code != 0 {
		t.Fatalf("exit code = %d, want 0 for a drifted section", code)
	}
}

func mustExplainCode(t *testing.T, args ...string) int {
	t.Helper()
	_, _, code := runExplain(t, args...)
	return code
}

func TestExplainUsageErrors(t *testing.T) {
	r := historyRepo(t)
	r.Branch("feature")

	for name, args := range map[string][]string{
		"no argument":                {},
		"two arguments":              {"usage", "flags"},
		"unknown section":            {"nope"},
		"unknown key":                {"README.md#nope"},
		"invalid format":             {"usage", "--format", "xml"},
		"ignore-revs-file with base": {"usage", "--base", "main", "--ignore-revs-file", writeRevs(t)},
		"empty base":                 {"usage", "--base", ""},
		"unknown base":               {"usage", "--base", "does-not-exist"},
		"missing ignore-revs-file":   {"usage", "--ignore-revs-file", filepath.Join(t.TempDir(), "missing")},
		"min-severity is not a flag": {"usage", "--min-severity", "high"},
		"warn-only is not a flag":    {"usage", "--warn-only"},
	} {
		t.Run(name, func(t *testing.T) {
			explainFails(t, args...)
		})
	}
}

func TestExplainUnknownAndAmbiguousSectionMessages(t *testing.T) {
	r := historyRepo(t)
	r.Write(".docdrift.yaml", "docs:\n  - README.md\n  - docs/*.md\n")
	r.CommitFile("docs/guide.md", "# Guide\n\n## Usage\n\nSee `src/lib.go`.\n", "second doc")

	msg := explainFails(t, "usage")
	for _, want := range []string{"ambiguous", "README.md#usage", "docs/guide.md#usage"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q lacks %q", msg, want)
		}
	}
	if msg := explainFails(t, "nope"); !strings.Contains(msg, "unknown section") {
		t.Errorf("message %q", msg)
	}
	if rep := explainJSON(t, "docs/guide.md#usage"); rep.Section.Doc != "docs/guide.md" {
		t.Fatalf("section = %+v", rep.Section)
	}
	// A bare anchor with and without "#" resolves like ack does.
	if rep := explainJSON(t, "#guide"); rep.Section.Key != "docs/guide.md#guide" {
		t.Fatalf("section = %+v", rep.Section)
	}
}

func TestExplainDoesNotWriteAnything(t *testing.T) {
	r := checkRepo(t)
	r.CommitFile("src/lib.go", newLib, "change")
	before := r.Git("status", "--porcelain", "--ignored")
	explainJSON(t, "usage", "--base", "main")
	explainJSON(t, "usage")
	if after := r.Git("status", "--porcelain", "--ignored"); after != before {
		t.Fatalf("working tree changed:\n%s\n->\n%s", before, after)
	}
	assertNoLock(t, r)
}

func TestExplainWorksFromASubdirectory(t *testing.T) {
	r := checkRepo(t)
	r.CommitFile("src/lib.go", newLib, "change lib")
	root := explainJSON(t, "usage", "--base", "main")

	if err := os.Chdir(filepath.Join(r.Dir, "src")); err != nil {
		t.Fatal(err)
	}
	sub := explainJSON(t, "usage", "--base", "main")
	if !reflect.DeepEqual(root, sub) {
		t.Fatalf("diff mode from a subdirectory differs:\n%+v\n%+v", root, sub)
	}
	assertExplainAgreesWithCheck(t, true)

	hist := explainJSON(t, "usage")
	if hist.Mode != "history" || hist.Verdict != "drift" {
		t.Fatalf("history from a subdirectory: %+v", hist)
	}
	assertExplainAgreesWithCheck(t, false)
}
