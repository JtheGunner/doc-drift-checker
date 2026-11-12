package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/cli"
)

// run executes the root command with args and returns the combined output.
func run(t *testing.T, version string, args ...string) (string, error) {
	t.Helper()

	var out bytes.Buffer
	cmd := cli.NewRootCmd(version)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)

	err := cmd.Execute()

	return out.String(), err
}

func TestRootCmdVersionFlagPrintsInjectedVersion(t *testing.T) {
	out, err := run(t, "1.2.3", "--version")
	if err != nil {
		t.Fatalf("--version returned error: %v", err)
	}

	if !strings.Contains(out, "1.2.3") {
		t.Errorf("--version output %q does not contain the injected version", out)
	}
}

func TestRootCmdHelpNamesTheTool(t *testing.T) {
	out, err := run(t, "dev", "--help")
	if err != nil {
		t.Fatalf("--help returned error: %v", err)
	}

	if !strings.Contains(out, "doc-drift-checker") {
		t.Errorf("--help output %q does not name the tool", out)
	}
}

func TestRootCmdWithoutArgsShowsHelp(t *testing.T) {
	out, err := run(t, "dev")
	if err != nil {
		t.Fatalf("running without args returned error: %v", err)
	}

	if !strings.Contains(out, "Usage:") {
		t.Errorf("output %q does not contain usage help", out)
	}
}

func TestRootCmdUnknownArgumentFails(t *testing.T) {
	_, err := run(t, "dev", "does-not-exist")
	if err == nil {
		t.Fatal("expected an error for an unknown argument, got nil")
	}
}
