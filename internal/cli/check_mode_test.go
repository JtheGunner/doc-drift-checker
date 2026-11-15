package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/cli"
)

// modeRepo is historyRepo with a config file and newer code on a feature
// branch, so history mode and diff mode (against main) both report Usage.
func modeRepo(t *testing.T, config string) {
	t.Helper()
	r := historyRepo(t)
	r.Branch("feature")
	r.CommitFile(".docdrift.yaml", config, "config")
	r.CommitFile("src/lib.go", newLib, "change code")
}

// runCmd runs a command and returns its stdout, exit code and error text.
func runCmd(t *testing.T, args ...string) (stdout string, code int, errText string) {
	t.Helper()
	var out bytes.Buffer
	cmd := cli.NewRootCmd("dev")
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.Execute()
	if err != nil {
		errText = err.Error()
	}
	return out.String(), cli.ExitCode(err), errText
}

func TestCheckModeDefault(t *testing.T) {
	tests := []struct {
		name     string
		config   string
		args     []string
		wantCode int
		wantErr  string
	}{
		{"unset key without flag runs history mode", "docs:\n  - README.md\n", nil, 1, ""},
		{"mode block without default runs history mode", "mode: {}\n", nil, 1, ""},
		{"history without flag runs history mode", "mode:\n  default: history\n", nil, 1, ""},
		{"diff without flag is a usage error", "mode:\n  default: diff\n", nil, 2, "mode.default is diff"},
		{"base wins over diff", "mode:\n  default: diff\n", []string{"--base", "main"}, 1, ""},
		{"base wins over history", "mode:\n  default: history\n", []string{"--base", "main"}, 1, ""},
		{"ignore-revs-file with base stays an error", "mode:\n  default: history\n", []string{"--base", "main", "--ignore-revs-file", "x"}, 2, "history mode"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modeRepo(t, tt.config)
			_, code, errText := runCmd(t, append([]string{"check", "--format", "json"}, tt.args...)...)
			if code != tt.wantCode {
				t.Fatalf("exit code = %d, want %d (%s)", code, tt.wantCode, errText)
			}
			if !strings.Contains(errText, tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", errText, tt.wantErr)
			}
		})
	}
}

func TestCheckBaseSelectsDiffModeDespiteHistoryDefault(t *testing.T) {
	modeRepo(t, "mode:\n  default: history\n")
	out, _, _ := runCmd(t, "check", "--format", "json", "--base", "main")
	if !strings.Contains(out, "\"diff\"") {
		t.Errorf("output does not report diff mode: %s", out)
	}
}

func TestExplainModeDefaultDiffWithoutBaseIsUsageError(t *testing.T) {
	modeRepo(t, "mode:\n  default: diff\n")
	_, code, errText := runCmd(t, "explain", "usage")
	if code != 2 || !strings.Contains(errText, "mode.default is diff") {
		t.Fatalf("code = %d, error = %q", code, errText)
	}
	if _, code, errText := runCmd(t, "explain", "usage", "--base", "main"); code != 0 {
		t.Errorf("explain with --base: code = %d (%s), want 0", code, errText)
	}
}
