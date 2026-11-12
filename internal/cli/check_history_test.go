package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/cli"
	"github.com/JtheGunner/doc-drift-checker/internal/testutil"
)

// historyRepo commits checkReadme and its code in one base commit on main.
// Every further testutil commit is one minute newer.
func historyRepo(t *testing.T) *testutil.Repo {
	t.Helper()
	r := testutil.NewRepo(t)
	r.Write("README.md", checkReadme)
	r.Write("src/lib.go", "package src\n")
	r.Write("src/flags.go", "package src\n")
	r.Write("other/o.go", "package other\n")
	r.Commit("base")
	t.Chdir(r.Dir)
	return r
}

const (
	newLib   = "package src\n\nfunc New() {}\n"
	newFlags = "package src\n\nfunc Flags() {}\n"
)

// editUsage changes a line inside the "Usage" section's own range.
func editUsage(readme string) string {
	return strings.Replace(readme, "to start.", "to start it.", 1)
}

// editFlags changes a line inside the "Flags" section.
func editFlags(readme string) string {
	return strings.Replace(readme, "See `src/flags.go`.", "See `src/flags.go` for all flags.", 1)
}

type historyReport struct {
	SchemaVersion int         `json:"schemaVersion"`
	Mode          string      `json:"mode"`
	Base          string      `json:"base"`
	Drifts        []jsonDrift `json:"drifts"`
}

// historyJSON runs check without --base and asserts the exit code.
func historyJSON(t *testing.T, wantCode int, args ...string) historyReport {
	t.Helper()
	out, errOut, code := runCheck(t, append([]string{"--format", "json"}, args...)...)
	if code != wantCode {
		t.Fatalf("exit code = %d, want %d\nstdout: %s\nstderr: %s", code, wantCode, out, errOut)
	}
	var rep historyReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	return rep
}

func writeRevs(t *testing.T, revs ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ignore-revs")
	content := "# formatting commits\n" + strings.Join(revs, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestHistoryCodeNewerThanDocIsDrift(t *testing.T) {
	r := historyRepo(t)
	r.CommitFile("src/lib.go", newLib, "change code")

	rep := historyJSON(t, cli.ExitDrift)
	expectSections(t, rep.Drifts, "Usage")
	if d := rep.Drifts[0]; d.Doc != "README.md" || d.Line != 3 || strings.Join(d.Files, ",") != "src/lib.go" {
		t.Fatalf("unexpected drift: %+v", d)
	}
}

func TestHistoryDocNewerThanCodeIsNoDrift(t *testing.T) {
	r := historyRepo(t)
	r.CommitFile("src/lib.go", newLib, "change code")
	r.CommitFile("README.md", editUsage(checkReadme), "update docs")

	expectSections(t, historyJSON(t, 0).Drifts)
}

func TestHistoryCodeAndDocInSameCommitIsNoDrift(t *testing.T) {
	r := historyRepo(t)
	r.Write("src/lib.go", newLib)
	r.Write("README.md", editUsage(checkReadme))
	r.Commit("both")

	expectSections(t, historyJSON(t, 0).Drifts)
}

func TestHistoryUnchangedRepoIsNoDrift(t *testing.T) {
	historyRepo(t)
	expectSections(t, historyJSON(t, 0).Drifts)
}

func TestHistoryDatesSectionsByLineNotByFile(t *testing.T) {
	r := historyRepo(t)
	r.CommitFile("src/lib.go", newLib, "change code")
	// The README file is now newer than lib.go, but only the Flags section was
	// edited: Usage is still older than its code.
	r.CommitFile("README.md", editFlags(checkReadme), "edit another section")

	expectSections(t, historyJSON(t, cli.ExitDrift).Drifts, "Usage")
}

func TestHistoryChildSectionEditDoesNotHideParentDrift(t *testing.T) {
	r := historyRepo(t)
	r.CommitFile("src/lib.go", newLib, "change code")
	r.CommitFile("README.md", editFlags(checkReadme), "edit child section")
	r.CommitFile("src/flags.go", newFlags, "change flags code")

	// Flags: its own text (t3) is older than flags.go (t4). Usage: older than lib.go.
	expectSections(t, historyJSON(t, cli.ExitDrift).Drifts, "Usage", "Flags")
}

func TestHistoryDocFormattingHidesDriftUnlessIgnored(t *testing.T) {
	r := historyRepo(t)
	r.CommitFile("src/lib.go", newLib, "change code")
	format := r.CommitFile("README.md", strings.Replace(checkReadme, "to start.", "to start. ", 1), "format docs")

	expectSections(t, historyJSON(t, 0).Drifts)
	rep := historyJSON(t, cli.ExitDrift, "--ignore-revs-file", writeRevs(t, format))
	expectSections(t, rep.Drifts, "Usage")
}

func TestHistoryCodeFormattingCausesFalseDriftUnlessIgnored(t *testing.T) {
	r := historyRepo(t)
	r.CommitFile("src/lib.go", newLib, "change code")
	r.CommitFile("README.md", editUsage(checkReadme), "update docs")
	format := r.CommitFile("src/lib.go", "package src\n\nfunc New() { }\n", "format code")

	expectSections(t, historyJSON(t, cli.ExitDrift).Drifts, "Usage")
	expectSections(t, historyJSON(t, 0, "--ignore-revs-file", writeRevs(t, format)).Drifts)
}

func TestHistoryIgnoredPathsAreNotReported(t *testing.T) {
	r := historyRepo(t)
	readme := "# Project\n\n## Usage\n\nRun `src/lib.go`.\n\n## Quality\n\n<!-- drift: src/lib_test.go, go.sum, vendor/x.go -->\n\nNotes.\n"
	r.Write("README.md", readme)
	r.Write("src/lib_test.go", "package src\n")
	r.Write("go.sum", "x\n")
	r.Write("vendor/x.go", "package vendor\n")
	r.Commit("docs and files")
	r.Write("src/lib_test.go", "package src\n\nvar x = 1\n")
	r.Write("go.sum", "y\n")
	r.Write("vendor/x.go", "package vendor\n\nvar y = 1\n")
	r.Commit("touch ignored files")

	expectSections(t, historyJSON(t, 0).Drifts)

	r.CommitFile("src/lib.go", newLib, "change code")
	expectSections(t, historyJSON(t, cli.ExitDrift).Drifts, "Usage")
}

func TestHistoryUnmappedSectionIsNotReported(t *testing.T) {
	r := historyRepo(t)
	r.CommitFile("src/unmapped.go", "package src\n", "unmapped code")
	r.CommitFile("src/lib.go", newLib, "change code")

	// Only Usage maps src/lib.go; Flags and Other see no newer code.
	expectSections(t, historyJSON(t, cli.ExitDrift).Drifts, "Usage")
}

func TestHistoryDeletedCodeIsNotConsidered(t *testing.T) {
	r := historyRepo(t)
	r.Remove("src/lib.go")
	r.Commit("delete code")

	expectSections(t, historyJSON(t, 0).Drifts)
}

func TestHistoryIgnoresWorkingTreeChanges(t *testing.T) {
	r := historyRepo(t)
	r.Write("src/lib.go", newLib)

	expectSections(t, historyJSON(t, 0).Drifts)
}

func TestHistoryUncommittedDocIsNotReported(t *testing.T) {
	r := historyRepo(t)
	r.CommitFile("src/lib.go", newLib, "change code")
	r.Write("NEW.md", "# New\n\nSee `src/lib.go`.\n")
	r.Git("add", "NEW.md")

	rep := historyJSON(t, cli.ExitDrift)
	for _, d := range rep.Drifts {
		if d.Doc == "NEW.md" {
			t.Fatalf("uncommitted doc must not be reported: %+v", d)
		}
	}
}

func TestHistorySquashMergeLosesOrdering(t *testing.T) {
	tests := []struct {
		name   string
		branch func(r *testutil.Repo)
	}{
		{"code then docs in the branch", func(r *testutil.Repo) {
			r.CommitFile("src/lib.go", newLib, "change code")
			r.CommitFile("README.md", editUsage(checkReadme), "update docs")
		}},
		{"docs then code in the branch", func(r *testutil.Repo) {
			r.CommitFile("README.md", editUsage(checkReadme), "update docs")
			r.CommitFile("src/lib.go", newLib, "change code")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := historyRepo(t)
			r.Branch("topic")
			tt.branch(r)
			r.Git("checkout", "--quiet", "main")
			r.Git("merge", "--squash", "topic")
			r.Commit("squash topic")

			// One squash commit touches code and docs: equal dates, no drift,
			// even where the unsquashed history had the code last.
			expectSections(t, historyJSON(t, 0).Drifts)

			// Code changed after the squash is newer again.
			r.CommitFile("src/lib.go", newLib+"\nvar X = 1\n", "later code change")
			expectSections(t, historyJSON(t, cli.ExitDrift).Drifts, "Usage")
		})
	}
}

func TestHistoryRewrittenHistoryChangesResult(t *testing.T) {
	r := historyRepo(t)
	r.CommitFile("src/lib.go", newLib, "change code")
	r.CommitFile("README.md", editUsage(checkReadme), "update docs")
	expectSections(t, historyJSON(t, 0).Drifts)

	// Replay the same two commits in the opposite order on a new branch. The
	// committer times are new, so the code is now the newest change.
	r.Git("checkout", "--quiet", "-b", "rewritten", "main~2")
	r.Tick()
	r.Git("cherry-pick", "main")
	r.Tick()
	r.Git("cherry-pick", "main~1")

	expectSections(t, historyJSON(t, cli.ExitDrift).Drifts, "Usage")
}

func TestHistoryShallowCloneIsError(t *testing.T) {
	r := historyRepo(t)
	r.CommitFile("src/lib.go", newLib, "change code")
	clone := r.ShallowClone(1)
	t.Chdir(clone.Dir)

	cmd := cli.NewRootCmd("dev")
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"check"})
	err := cmd.Execute()
	if cli.ExitCode(err) != cli.ExitFailure {
		t.Fatalf("exit code = %d, want %d (err: %v)", cli.ExitCode(err), cli.ExitFailure, err)
	}
	if err == nil || !strings.Contains(err.Error(), "git fetch --unshallow") {
		t.Fatalf("error must hint at git fetch --unshallow, got %v", err)
	}
}

func TestHistoryIgnoreRevsWithBaseIsUsageError(t *testing.T) {
	r := historyRepo(t)
	r.Branch("feature")
	revs := writeRevs(t, strings.Repeat("a", 40))
	out, _, code := runCheck(t, "--base", "main", "--ignore-revs-file", revs)
	if code != cli.ExitFailure || out != "" {
		t.Fatalf("exit code = %d (stdout %q), want %d and no output", code, out, cli.ExitFailure)
	}
}

func TestHistoryIgnoreRevsFileErrors(t *testing.T) {
	historyRepo(t)
	dir := t.TempDir()
	malformed := filepath.Join(dir, "malformed")
	if err := os.WriteFile(malformed, []byte("# header\nnot-a-hash\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, path, wantMsg string
	}{
		{"missing file", filepath.Join(dir, "missing"), "missing"},
		{"directory", dir, "regular file"},
		{"malformed line", malformed, "line 2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := cli.NewRootCmd("dev")
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs([]string{"check", "--ignore-revs-file", tt.path})
			err := cmd.Execute()
			if cli.ExitCode(err) != cli.ExitFailure || err == nil || !strings.Contains(err.Error(), tt.wantMsg) {
				t.Fatalf("exit code = %d, err = %v; want %d naming %q", cli.ExitCode(err), err, cli.ExitFailure, tt.wantMsg)
			}
		})
	}
}

func TestHistoryWarnOnlyExitsZero(t *testing.T) {
	r := historyRepo(t)
	r.CommitFile("src/lib.go", newLib, "change code")

	expectSections(t, historyJSON(t, 0, "--warn-only").Drifts, "Usage")
}

func TestHistoryJSONHasModeAndEmptyBase(t *testing.T) {
	historyRepo(t)
	out, _, code := runCheck(t, "--format", "json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	want := "{\n  \"schemaVersion\": 1,\n  \"mode\": \"history\",\n  \"base\": \"\",\n  \"drifts\": []\n}\n"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestHistoryTextOutputMatchesDiffMode(t *testing.T) {
	r := historyRepo(t)
	r.CommitFile("src/lib.go", newLib, "change code")

	out, _, code := runCheck(t)
	if code != cli.ExitDrift {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitDrift)
	}
	want := "[low] Usage\n  README.md:3\n  changed code:\n    src/lib.go\n"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestHistoryInvalidFormatIsUsageError(t *testing.T) {
	historyRepo(t)
	if _, _, code := runCheck(t, "--format", "xml"); code != cli.ExitFailure {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitFailure)
	}
}
