package cli_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/cli"
	"github.com/JtheGunner/doc-drift-checker/internal/testutil"
)

const checkReadme = "# Project\n\n## Usage\n\nRun `src/lib.go` to start.\n\n### Flags\n\nSee `src/flags.go`.\n\n## Other\n\n<!-- drift: other/*.go -->\n\nOther things.\n"

// checkRepo commits a base state on main and switches to a feature branch.
func checkRepo(t *testing.T) *testutil.Repo {
	t.Helper()
	r := testutil.NewRepo(t)
	r.Write("README.md", checkReadme)
	r.Write("src/lib.go", "package src\n\nfunc Lib() {}\n")
	r.Write("src/flags.go", "package src\n")
	r.Write("other/o.go", "package other\n")
	r.Commit("base")
	r.Branch("feature")
	t.Chdir(r.Dir)
	return r
}

// runCheck runs the check command and returns stdout, stderr and the exit code.
func runCheck(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd := cli.NewRootCmd("dev")
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(append([]string{"check"}, args...))
	err := cmd.Execute()
	return out.String(), errOut.String(), cli.ExitCode(err)
}

type jsonDrift struct {
	Doc     string   `json:"doc"`
	Section string   `json:"section"`
	Line    int      `json:"line"`
	Files   []string `json:"files"`
}

func checkJSON(t *testing.T, wantCode int, args ...string) []jsonDrift {
	t.Helper()
	out, errOut, code := runCheck(t, append([]string{"--base", "main", "--format", "json"}, args...)...)
	if code != wantCode {
		t.Fatalf("exit code = %d, want %d\nstdout: %s\nstderr: %s", code, wantCode, out, errOut)
	}
	var report struct {
		SchemaVersion int         `json:"schemaVersion"`
		Base          string      `json:"base"`
		Drifts        []jsonDrift `json:"drifts"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if report.SchemaVersion != 1 || report.Base != "main" {
		t.Fatalf("unexpected header: %+v", report)
	}
	return report.Drifts
}

func sections(drifts []jsonDrift) []string {
	names := []string{}
	for _, d := range drifts {
		names = append(names, d.Section)
	}
	return names
}

func expectSections(t *testing.T, drifts []jsonDrift, want ...string) {
	t.Helper()
	got := strings.Join(sections(drifts), ",")
	if got != strings.Join(want, ",") {
		t.Fatalf("drifted sections = [%s], want %v", got, want)
	}
}

func TestCheckOnlyCodeChanged(t *testing.T) {
	r := checkRepo(t)
	r.CommitFile("src/lib.go", "package src\n\nfunc New() {}\n", "change code")

	drifts := checkJSON(t, cli.ExitDrift)
	expectSections(t, drifts, "Usage")
	if d := drifts[0]; d.Doc != "README.md" || d.Line != 3 || strings.Join(d.Files, ",") != "src/lib.go" {
		t.Fatalf("unexpected drift: %+v", d)
	}
}

func TestCheckBothChanged(t *testing.T) {
	r := checkRepo(t)
	r.Write("src/lib.go", "package src\n\nfunc New() {}\n")
	r.Write("README.md", strings.Replace(checkReadme, "to start", "to start it", 1))
	r.Commit("both")

	expectSections(t, checkJSON(t, 0))
}

func TestCheckOnlyDocsChanged(t *testing.T) {
	r := checkRepo(t)
	r.CommitFile("README.md", strings.Replace(checkReadme, "to start", "to start it", 1), "docs")

	expectSections(t, checkJSON(t, 0))
}

func TestCheckCodeRenamed(t *testing.T) {
	r := checkRepo(t)
	r.Move("src/lib.go", "src/library.go")
	r.Commit("rename")

	// The new path is not mentioned in the README, the old one is: the rename
	// away from a mapped path must still trigger.
	drifts := checkJSON(t, cli.ExitDrift)
	expectSections(t, drifts, "Usage")
	if got := strings.Join(drifts[0].Files, ","); got != "src/lib.go -> src/library.go" {
		t.Fatalf("files = %q", got)
	}
}

func TestCheckCodeRenamedWithinMappedGlob(t *testing.T) {
	r := checkRepo(t)
	r.Move("other/o.go", "other/p.go")
	r.Commit("rename")

	expectSections(t, checkJSON(t, cli.ExitDrift), "Other")
}

func TestCheckCodeDeleted(t *testing.T) {
	r := checkRepo(t)
	r.Remove("src/lib.go")
	r.Commit("delete")

	drifts := checkJSON(t, cli.ExitDrift)
	expectSections(t, drifts, "Usage")
	if got := strings.Join(drifts[0].Files, ","); got != "src/lib.go" {
		t.Fatalf("files = %q", got)
	}
}

func TestCheckUnrelatedCodeIsNotReported(t *testing.T) {
	r := checkRepo(t)
	r.CommitFile("src/unmapped.go", "package src\n", "unrelated file")
	r.CommitFile("other/o.go", "package other\n\nvar X = 1\n", "other code")

	expectSections(t, checkJSON(t, cli.ExitDrift), "Other")
}

func TestCheckChildEditDoesNotHideParentDrift(t *testing.T) {
	r := checkRepo(t)
	r.Write("src/lib.go", "package src\n\nfunc New() {}\n")
	r.Write("README.md", strings.Replace(checkReadme, "See `src/flags.go`.", "See `src/flags.go` now.", 1))
	r.Commit("code and child doc")

	expectSections(t, checkJSON(t, cli.ExitDrift), "Usage")
}

func TestCheckParentEditDoesNotHideChildDrift(t *testing.T) {
	r := checkRepo(t)
	r.Write("src/flags.go", "package src\n\nfunc Flags() {}\n")
	r.Write("README.md", strings.Replace(checkReadme, "to start", "to start it", 1))
	r.Commit("child code and parent doc")

	expectSections(t, checkJSON(t, cli.ExitDrift), "Flags")
}

func TestCheckNewDocFileHasNoDrift(t *testing.T) {
	r := checkRepo(t)
	r.Write(".docdrift.yaml", "docs:\n  - README.md\n  - docs/*.md\n")
	r.Write("docs/new.md", "# New\n\nSee `src/lib.go`.\n")
	r.Write("src/lib.go", "package src\n\nfunc New() {}\n")
	r.Write("README.md", strings.Replace(checkReadme, "to start", "to start it", 1))
	r.Commit("add doc")

	expectSections(t, checkJSON(t, 0))
}

func TestCheckReadmeOnlyChangeNeverDrifts(t *testing.T) {
	r := checkRepo(t)
	r.CommitFile("README.md", checkReadme+"\nMore text about `README.md`.\n", "readme")

	expectSections(t, checkJSON(t, 0))
}

func TestCheckWarnOnlyForcesZero(t *testing.T) {
	r := checkRepo(t)
	r.CommitFile("src/lib.go", "package src\n\nfunc New() {}\n", "change code")

	drifts := checkJSON(t, 0, "--warn-only")
	expectSections(t, drifts, "Usage")
}

func TestCheckWarnOnlyStillFailsOnErrors(t *testing.T) {
	checkRepo(t)
	if _, _, code := runCheck(t, "--base", "nope", "--warn-only"); code != cli.ExitFailure {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitFailure)
	}
}

func TestCheckUnknownBaseIsRuntimeError(t *testing.T) {
	checkRepo(t)
	if _, _, code := runCheck(t, "--base", "does-not-exist"); code != cli.ExitFailure {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitFailure)
	}
}

func TestCheckInvalidFormatIsUsageError(t *testing.T) {
	checkRepo(t)
	if _, _, code := runCheck(t, "--base", "main", "--format", "xml"); code != cli.ExitFailure {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitFailure)
	}
}

func TestCheckTextOutput(t *testing.T) {
	r := checkRepo(t)
	r.CommitFile("src/lib.go", "package src\n\nfunc New() {}\n", "change code")

	out, _, code := runCheck(t, "--base", "main")
	if code != cli.ExitDrift {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitDrift)
	}
	want := "Usage\n  README.md:3\n  changed code:\n    src/lib.go\n"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestCheckNoDriftJSONHasEmptyArray(t *testing.T) {
	checkRepo(t)
	out, _, code := runCheck(t, "--base", "main", "--format", "json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	want := "{\n  \"schemaVersion\": 1,\n  \"mode\": \"diff\",\n  \"base\": \"main\",\n  \"drifts\": []\n}\n"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

// relevanceReadme maps one section to files in ignored locations and one to a relevant file.
const relevanceReadme = "# Project\n\n## Usage\n\nRun `src/lib.go` to start.\n\n## Quality\n\n<!-- drift: src/lib_test.go, go.sum, vendor/**, src/api.pb.go -->\n\nQuality notes.\n"

func relevanceRepo(t *testing.T, extra ...string) *testutil.Repo {
	t.Helper()
	r := testutil.NewRepo(t)
	r.Write("README.md", relevanceReadme)
	r.Write("src/lib.go", "package src\n\nfunc A() {\n\tx := 1\n}\n")
	r.Write("src/lib_test.go", "package src\n")
	r.Write("go.sum", "a\n")
	r.Write("vendor/mod/m.go", "package mod\n")
	r.Write("src/api.pb.go", "package src\n")
	for i := 0; i+1 < len(extra); i += 2 {
		r.Write(extra[i], extra[i+1])
	}
	r.Commit("base")
	r.Branch("feature")
	t.Chdir(r.Dir)
	return r
}

func TestCheckIgnoredFilesDoNotDrift(t *testing.T) {
	tests := []struct{ name, path string }{
		{"test file", "src/lib_test.go"},
		{"lockfile", "go.sum"},
		{"vendor path", "vendor/mod/m.go"},
		{"generated file", "src/api.pb.go"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := relevanceRepo(t)
			r.CommitFile(tt.path, "changed content\n", "change ignored file")
			expectSections(t, checkJSON(t, 0))
		})
	}
}

func TestCheckIgnoredAndRelevantFilesListOnlyRelevant(t *testing.T) {
	r := relevanceRepo(t)
	r.Write("src/lib_test.go", "package src\n\nvar X = 1\n")
	r.Write("src/lib.go", "package src\n\nfunc B() {}\n")
	r.Commit("both")

	drifts := checkJSON(t, cli.ExitDrift)
	expectSections(t, drifts, "Usage")
	if got := strings.Join(drifts[0].Files, ","); got != "src/lib.go" {
		t.Fatalf("files = %q, want only src/lib.go", got)
	}
}

func TestCheckWhitespaceOnlyChangeDoesNotDrift(t *testing.T) {
	r := relevanceRepo(t)
	r.CommitFile("src/lib.go", "package src\n\n\nfunc A() {\n    x := 1   \n}\n", "reformat")
	expectSections(t, checkJSON(t, 0))
}

func TestCheckWhitespaceAndRealChangeDrifts(t *testing.T) {
	r := relevanceRepo(t)
	r.CommitFile("src/lib.go", "package src\n\nfunc A(n int) {\n    x := 1\n}\n", "reformat and change signature")
	expectSections(t, checkJSON(t, cli.ExitDrift), "Usage")
}

func TestCheckCustomIgnoreReplacesDefaults(t *testing.T) {
	r := relevanceRepo(t, ".docdrift.yaml", "ignore: [\"docs/**\"]\n")
	r.CommitFile("src/lib_test.go", "package src\n\nvar X = 1\n", "change test file")
	// The default list is replaced, so the test file is relevant now.
	expectSections(t, checkJSON(t, cli.ExitDrift), "Quality")
}

func TestCheckCustomIgnoreAppliesToCustomPaths(t *testing.T) {
	r := relevanceRepo(t, ".docdrift.yaml", "ignore: [\"src/lib.go\"]\n")
	r.CommitFile("src/lib.go", "package src\n\nfunc B() {}\n", "change ignored by config")
	expectSections(t, checkJSON(t, 0))
}

func TestCheckEmptyIgnoreDisablesPathIgnoring(t *testing.T) {
	r := relevanceRepo(t, ".docdrift.yaml", "ignore: []\n")
	r.CommitFile("go.sum", "b\n", "change lockfile")
	expectSections(t, checkJSON(t, cli.ExitDrift), "Quality")
}

func TestCheckRenameAcrossIgnoredBoundaryStaysRelevant(t *testing.T) {
	t.Run("into ignored path", func(t *testing.T) {
		r := relevanceRepo(t)
		r.Move("src/lib.go", "src/lib_gen.go")
		r.Commit("rename into generated name")
		expectSections(t, checkJSON(t, cli.ExitDrift), "Usage")
	})
	t.Run("out of ignored path", func(t *testing.T) {
		r := relevanceRepo(t)
		r.Move("src/lib_test.go", "src/lib_check.go")
		r.Commit("rename out of test name")
		expectSections(t, checkJSON(t, cli.ExitDrift), "Quality")
	})
	t.Run("within ignored paths", func(t *testing.T) {
		r := relevanceRepo(t)
		r.Move("src/lib_test.go", "src/other_test.go")
		r.Commit("rename test file")
		expectSections(t, checkJSON(t, 0))
	})
}
