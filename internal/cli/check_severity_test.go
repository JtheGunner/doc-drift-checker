package cli_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/cli"
	"github.com/JtheGunner/doc-drift-checker/internal/testutil"
)

const severityReadme = "# Project\n\n## Usage\n\nRun with `--verbose` and set `APP_TOKEN`; see `src/lib.go`.\n\n## Notes\n\nThe `Execute` function lives in `src/api.go`.\n"

const severityLib = "package src\n\nconst VerboseFlag = \"--verbose\"\n\nconst TokenEnv = \"APP_TOKEN\"\n"

// severityRepo commits the base state on main and switches to a feature branch.
func severityRepo(t *testing.T, config string) *testutil.Repo {
	t.Helper()
	return severityRepoWithReadme(t, severityReadme, config)
}

func severityRepoWithReadme(t *testing.T, readme, config string) *testutil.Repo {
	t.Helper()
	r := testutil.NewRepo(t)
	r.Write("README.md", readme)
	r.Write("src/lib.go", severityLib)
	r.Write("src/api.go", "package src\n\nfunc Execute() {}\n")
	if config != "" {
		r.Write(".docdrift.yaml", config)
	}
	r.Commit("base")
	r.Branch("feature")
	t.Chdir(r.Dir)
	return r
}

type severityDrift struct {
	Section          string          `json:"section"`
	Severity         string          `json:"severity"`
	BrokenReferences json.RawMessage `json:"brokenReferences"`
}

func severityJSON(t *testing.T, wantCode int, args ...string) []severityDrift {
	t.Helper()
	out, errOut, code := runCheck(t, append([]string{"--format", "json"}, args...)...)
	if code != wantCode {
		t.Fatalf("exit code = %d, want %d\nstdout: %s\nstderr: %s", code, wantCode, out, errOut)
	}
	var rep struct {
		Drifts []severityDrift `json:"drifts"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	return rep.Drifts
}

func removeFlag(r *testutil.Repo) {
	r.CommitFile("src/lib.go", "package src\n\nconst VerboseFlag = \"--chatty\"\n\nconst TokenEnv = \"APP_TOKEN\"\n", "rename flag")
}

func addUnrelatedExport(r *testutil.Repo) {
	r.CommitFile("src/lib.go", severityLib+"\nconst Retries = 3\n", "add constant")
}

func TestSeverityRemovedFlagIsHigh(t *testing.T) {
	r := severityRepo(t, "")
	removeFlag(r)

	drifts := severityJSON(t, cli.ExitDrift, "--base", "main")
	if len(drifts) != 1 || drifts[0].Section != "Usage" || drifts[0].Severity != "high" {
		t.Fatalf("drifts = %+v", drifts)
	}
	var broken []struct {
		Kind  string `json:"kind"`
		Value string `json:"value"`
		Line  int    `json:"line"`
	}
	if err := json.Unmarshal(drifts[0].BrokenReferences, &broken); err != nil {
		t.Fatal(err)
	}
	if len(broken) != 1 || broken[0].Kind != "flag" || broken[0].Value != "--verbose" || broken[0].Line != 5 {
		t.Fatalf("brokenReferences = %+v", broken)
	}
}

func TestSeverityTextOutput(t *testing.T) {
	r := severityRepo(t, "")
	removeFlag(r)

	out, _, code := runCheck(t, "--base", "main")
	if code != cli.ExitDrift {
		t.Fatalf("exit code = %d", code)
	}
	want := "[high] Usage\n  README.md:3\n  changed code:\n    src/lib.go\n  broken references:\n    flag --verbose (README.md:5)\n"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestSeverityRemovedEnvVarIsHigh(t *testing.T) {
	r := severityRepo(t, "")
	r.CommitFile("src/lib.go", "package src\n\nconst VerboseFlag = \"--verbose\"\n\nconst TokenEnv = \"APP_KEY\"\n", "rename env var")

	drifts := severityJSON(t, cli.ExitDrift, "--base", "main")
	if len(drifts) != 1 || drifts[0].Severity != "high" || !strings.Contains(string(drifts[0].BrokenReferences), "APP_TOKEN") {
		t.Fatalf("drifts = %+v", drifts)
	}
}

func TestSeverityRemovedIdentifierIsHigh(t *testing.T) {
	r := severityRepo(t, "")
	r.CommitFile("src/api.go", "package src\n\nfunc Perform() {}\n", "rename function")

	drifts := severityJSON(t, cli.ExitDrift, "--base", "main")
	if len(drifts) != 1 || drifts[0].Section != "Notes" || drifts[0].Severity != "high" ||
		!strings.Contains(string(drifts[0].BrokenReferences), "Execute") {
		t.Fatalf("drifts = %+v", drifts)
	}
}

func TestSeverityUnrelatedChangeIsLow(t *testing.T) {
	r := severityRepo(t, "")
	addUnrelatedExport(r)

	drifts := severityJSON(t, cli.ExitDrift, "--base", "main")
	if len(drifts) != 1 || drifts[0].Severity != "low" || string(drifts[0].BrokenReferences) != "[]" {
		t.Fatalf("drifts = %+v", drifts)
	}
}

func TestSeverityRenamedFileKeepingReferenceIsLow(t *testing.T) {
	r := severityRepo(t, "")
	r.Move("src/lib.go", "src/library.go")
	r.Commit("rename file")

	drifts := severityJSON(t, cli.ExitDrift, "--base", "main")
	if len(drifts) != 1 || drifts[0].Severity != "low" {
		t.Fatalf("drifts = %+v (the reference moved with the file)", drifts)
	}
}

func TestSeverityDeletedFileHoldingReferenceIsHigh(t *testing.T) {
	r := severityRepo(t, "")
	r.Remove("src/lib.go")
	r.Commit("delete file")

	drifts := severityJSON(t, cli.ExitDrift, "--base", "main")
	if len(drifts) != 1 || drifts[0].Severity != "high" {
		t.Fatalf("drifts = %+v", drifts)
	}
}

func TestSeverityReferenceNeverInCodeIsLow(t *testing.T) {
	r := severityRepoWithReadme(t, strings.Replace(severityReadme, "`--verbose`", "`--ghost`", 1), "")
	removeFlag(r)

	drifts := severityJSON(t, cli.ExitDrift, "--base", "main")
	if len(drifts) != 1 || drifts[0].Severity != "low" {
		t.Fatalf("drifts = %+v", drifts)
	}
}

func TestSeverityPathBasedLanguageFile(t *testing.T) {
	r := testutil.NewRepo(t)
	r.Write("README.md", "# P\n\n## Run\n\nUse `--fast` via `bin/run.sh`.\n")
	r.Write("bin/run.sh", "#!/bin/sh\nmode=\"--fast\"\n")
	r.Commit("base")
	r.Branch("feature")
	t.Chdir(r.Dir)
	r.CommitFile("bin/run.sh", "#!/bin/sh\nmode=\"--slow\"\n", "change")

	drifts := severityJSON(t, cli.ExitDrift, "--base", "main")
	if len(drifts) != 1 || drifts[0].Severity != "high" {
		t.Fatalf("drifts = %+v", drifts)
	}
}

func TestMinSeverityHighIgnoresLowDriftsForExitCode(t *testing.T) {
	r := severityRepo(t, "")
	addUnrelatedExport(r)

	drifts := severityJSON(t, 0, "--base", "main", "--min-severity", "high")
	if len(drifts) != 1 || drifts[0].Severity != "low" {
		t.Fatalf("low drifts must still be printed: %+v", drifts)
	}
	out, _, code := runCheck(t, "--base", "main", "--min-severity", "high")
	if code != 0 || !strings.HasPrefix(out, "[low] Usage") {
		t.Fatalf("code = %d, out = %q", code, out)
	}
}

func TestMinSeverityHighFailsOnHighDrift(t *testing.T) {
	r := severityRepo(t, "")
	removeFlag(r)

	severityJSON(t, cli.ExitDrift, "--base", "main", "--min-severity", "high")
}

func TestMinSeverityLowFailsOnLowDrift(t *testing.T) {
	r := severityRepo(t, "")
	addUnrelatedExport(r)

	severityJSON(t, cli.ExitDrift, "--base", "main", "--min-severity", "low")
}

func TestMinSeverityFromConfigAndFlagOverride(t *testing.T) {
	r := severityRepo(t, "severity:\n  fail-on: high\n")
	addUnrelatedExport(r)

	severityJSON(t, 0, "--base", "main")
	severityJSON(t, cli.ExitDrift, "--base", "main", "--min-severity", "low")
}

func TestMinSeverityConfigHighStillFailsOnHigh(t *testing.T) {
	r := severityRepo(t, "severity:\n  fail-on: high\n")
	removeFlag(r)

	severityJSON(t, cli.ExitDrift, "--base", "main")
	severityJSON(t, 0, "--base", "main", "--min-severity", "high", "--warn-only")
}

func TestMinSeverityFlagOverridesConfigToHigh(t *testing.T) {
	r := severityRepo(t, "severity:\n  fail-on: low\n")
	addUnrelatedExport(r)

	severityJSON(t, cli.ExitDrift, "--base", "main")
	severityJSON(t, 0, "--base", "main", "--min-severity", "high")
}

func TestMinSeverityInvalidValueIsUsageError(t *testing.T) {
	r := severityRepo(t, "")
	addUnrelatedExport(r)

	for _, args := range [][]string{
		{"--base", "main", "--min-severity", "medium"},
		{"--base", "main", "--min-severity", ""},
		{"--min-severity", "urgent"},
	} {
		var out, errOut bytes.Buffer
		cmd := cli.NewRootCmd("dev")
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)
		cmd.SetArgs(append([]string{"check"}, args...))
		err := cmd.Execute()
		if code := cli.ExitCode(err); code != cli.ExitFailure {
			t.Fatalf("args %v: exit code = %d, want %d", args, code, cli.ExitFailure)
		}
		if out.Len() != 0 {
			t.Fatalf("args %v: nothing may be reported on a usage error, got %q", args, out.String())
		}
	}
}

func TestWarnOnlyForcesZeroForHighDrift(t *testing.T) {
	r := severityRepo(t, "")
	removeFlag(r)

	drifts := severityJSON(t, 0, "--base", "main", "--warn-only")
	if len(drifts) != 1 || drifts[0].Severity != "high" {
		t.Fatalf("drifts = %+v", drifts)
	}
}

func TestHistoryModeDriftsAreAllLow(t *testing.T) {
	r := severityRepo(t, "")
	removeFlag(r)

	drifts := severityJSON(t, cli.ExitDrift)
	if len(drifts) == 0 {
		t.Fatal("expected a history-mode drift")
	}
	for _, d := range drifts {
		if d.Severity != "low" || string(d.BrokenReferences) != "[]" {
			t.Fatalf("drift = %+v", d)
		}
	}
	severityJSON(t, 0, "--min-severity", "high")
}
