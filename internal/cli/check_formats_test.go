package cli_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/cli"
)

func TestCheckGitHubFormatDiffMode(t *testing.T) {
	r := checkRepo(t)
	r.CommitFile("src/lib.go", "package src\n\nfunc New() {}\n", "change code")

	out, errOut, code := runCheck(t, "--base", "main", "--format", "github")
	if code != cli.ExitDrift {
		t.Fatalf("exit code = %d, want %d\n%s", code, cli.ExitDrift, errOut)
	}
	want := "::warning file=README.md,line=3,title=doc-drift::Section \"Usage\" may be outdated: " +
		"linked code changed but the section did not (changed code: src/lib.go).\n"
	if out != want {
		t.Fatalf("got %q\nwant %q", out, want)
	}
}

func TestCheckGitHubFormatHistoryMode(t *testing.T) {
	r := historyRepo(t)
	r.CommitFile("src/lib.go", newLib, "newer code")

	out, errOut, code := runCheck(t, "--format", "github")
	if code != cli.ExitDrift {
		t.Fatalf("exit code = %d, want %d\n%s", code, cli.ExitDrift, errOut)
	}
	if !strings.HasPrefix(out, "::warning file=README.md,line=3,") || strings.Count(out, "\n") != 1 {
		t.Fatalf("unexpected output %q", out)
	}
}

func TestCheckGitHubFormatNoDriftIsSilent(t *testing.T) {
	checkRepo(t)
	out, _, code := runCheck(t, "--base", "main", "--format", "github")
	if code != 0 || out != "" {
		t.Fatalf("exit code = %d, output = %q, want 0 and nothing", code, out)
	}
}

func TestCheckGitHubFormatHighSeverityIsError(t *testing.T) {
	r := checkRepo(t)
	r.Write("README.md", strings.Replace(checkReadme, "Run `src/lib.go` to start.", "Run `src/lib.go` with `--verbose`.", 1))
	r.Write("src/lib.go", "package src\n\nfunc Lib() { _ = \"--verbose\" }\n")
	r.Commit("add flag on main-like base")
	r.Git("branch", "-f", "main", "HEAD")
	r.Write("src/lib.go", "package src\n\nfunc Lib2() {}\n")
	r.Commit("remove flag")

	out, _, code := runCheck(t, "--base", "main", "--format", "github")
	if code != cli.ExitDrift || !strings.HasPrefix(out, "::error ") || !strings.Contains(out, "flag --verbose") {
		t.Fatalf("exit code = %d, output %q", code, out)
	}
}

func TestCheckSARIFFormat(t *testing.T) {
	for name, history := range map[string]bool{"diff": false, "history": true} {
		t.Run(name, func(t *testing.T) {
			args := []string{"--format", "sarif"}
			if history {
				r := historyRepo(t)
				r.CommitFile("src/lib.go", newLib, "newer code")
			} else {
				r := checkRepo(t)
				r.CommitFile("src/lib.go", "package src\n\nfunc New() {}\n", "change code")
				args = append(args, "--base", "main")
			}
			out, errOut, code := runCheck(t, args...)
			if code != cli.ExitDrift {
				t.Fatalf("exit code = %d, want %d\n%s", code, cli.ExitDrift, errOut)
			}
			var log struct {
				Version string `json:"version"`
				Runs    []struct {
					Results []struct {
						Level string `json:"level"`
					} `json:"results"`
				} `json:"runs"`
			}
			if err := json.Unmarshal([]byte(out), &log); err != nil {
				t.Fatalf("invalid JSON: %v\n%s", err, out)
			}
			if log.Version != "2.1.0" || len(log.Runs) != 1 || len(log.Runs[0].Results) != 1 || log.Runs[0].Results[0].Level != "warning" {
				t.Fatalf("unexpected SARIF: %s", out)
			}
		})
	}
}

func TestCheckSARIFFormatNoDriftIsEmptyResults(t *testing.T) {
	checkRepo(t)
	out, _, code := runCheck(t, "--base", "main", "--format", "sarif")
	if code != 0 || !strings.Contains(out, `"results": []`) {
		t.Fatalf("exit code = %d, output %s", code, out)
	}
}

func TestCheckNewFormatsKeepWarnOnlyAndMinSeverity(t *testing.T) {
	r := checkRepo(t)
	r.CommitFile("src/lib.go", "package src\n\nfunc New() {}\n", "change code")

	for _, format := range []string{"sarif", "github"} {
		out, _, code := runCheck(t, "--base", "main", "--format", format, "--warn-only")
		if code != 0 || out == "" {
			t.Errorf("%s --warn-only: exit code = %d, output %q", format, code, out)
		}
		out, _, code = runCheck(t, "--base", "main", "--format", format, "--min-severity", "high")
		if code != 0 || out == "" {
			t.Errorf("%s --min-severity high: exit code = %d, output %q", format, code, out)
		}
	}
}

func TestCheckUnknownFormatVariantsAreUsageErrors(t *testing.T) {
	checkRepo(t)
	for _, format := range []string{"xml", "SARIF", ""} {
		if _, _, code := runCheck(t, "--base", "main", "--format", format); code != cli.ExitFailure {
			t.Errorf("--format %q: exit code = %d, want %d", format, code, cli.ExitFailure)
		}
	}
}

// A heading and a file name that try to inject workflow commands must come
// out as one harmless annotation line per drift.
func TestCheckGitHubFormatHostileHeadingAndFileName(t *testing.T) {
	r := checkRepo(t)
	hostile := "# Project\n\n## Evil ::error::pwned %0A ,:\n\n<!-- drift: src/we* -->\n\nText.\n"
	r.Write("README.md", hostile)
	r.Write("src/we%ird,na:me.go", "package src\n")
	r.Commit("hostile base")
	r.Git("branch", "-f", "main", "HEAD")
	r.CommitFile("src/we%ird,na:me.go", "package src\n\nfunc X() {}\n", "code")

	out, _, code := runCheck(t, "--base", "main", "--format", "github")
	if code != cli.ExitDrift {
		t.Fatalf("exit code = %d, output %q", code, out)
	}
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		if !strings.HasPrefix(line, "::warning file=README.md,line=") && !strings.HasPrefix(line, "::error file=README.md,line=") {
			t.Fatalf("line is not an annotation of the doc: %q", line)
		}
	}
	if !strings.Contains(out, "%25ird") {
		t.Fatalf("percent in file name not escaped: %q", out)
	}
}
