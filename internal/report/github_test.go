package report_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/detect"
	"github.com/JtheGunner/doc-drift-checker/internal/report"
)

func githubOf(t *testing.T, drifts []detect.Drift) string {
	t.Helper()
	var buf bytes.Buffer
	if err := report.WriteGitHub(&buf, drifts); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestWriteGitHubGolden(t *testing.T) {
	golden(t, "github_drifts.golden", githubOf(t, sample))
}

func TestWriteGitHubEmpty(t *testing.T) {
	if out := githubOf(t, nil); out != "" {
		t.Fatalf("want no output, got %q", out)
	}
}

func TestWriteGitHubSeverityPicksCommand(t *testing.T) {
	lines := strings.Split(strings.TrimSuffix(githubOf(t, sample), "\n"), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "::error ") || !strings.HasPrefix(lines[1], "::warning ") {
		t.Fatalf("unexpected lines: %q", lines)
	}
}

func TestWriteGitHubEscapesProperties(t *testing.T) {
	out := githubOf(t, []detect.Drift{{Doc: "a:b,c%d.md", Section: "S", Line: 2, Files: []string{"x.go"}}})
	if !strings.Contains(out, "file=a%3Ab%2Cc%25d.md,line=2,title=doc-drift::") {
		t.Fatalf("properties not escaped: %q", out)
	}
}

func TestWriteGitHubMessageEscapesPercent(t *testing.T) {
	out := githubOf(t, []detect.Drift{{Doc: "a.md", Section: "100% done", Line: 1, Files: []string{"x.go"}}})
	if !strings.Contains(out, "100%25 done") {
		t.Fatalf("percent not escaped: %q", out)
	}
}

// Untrusted text must never start a second workflow command: every output
// line is exactly one annotation, whatever the headings and paths contain.
func TestWriteGitHubHostileInputCannotInjectCommands(t *testing.T) {
	out := githubOf(t, hostileDrifts)
	if !strings.HasSuffix(out, "\n") || strings.Count(out, "\n") != 1 {
		t.Fatalf("want exactly one line, got %q", out)
	}
	if strings.Contains(out, "\r") || strings.Contains(out, "\x1b") || strings.Contains(out, "\x00") {
		t.Fatalf("raw control characters: %q", out)
	}
	if strings.Count(out, "::") != 2 {
		// Only the command prefix and the property/message separator may appear
		// unescaped; the payload's "::" must still be harmless because it can
		// only appear after the separator on the same line.
		head, _, _ := strings.Cut(out, "::")
		if head != "" {
			t.Fatalf("line does not start with a command: %q", out)
		}
	}
	file := strings.TrimPrefix(strings.SplitN(out, ",line=", 2)[0], "::error file=")
	if strings.ContainsAny(file, ":,\n\r") {
		t.Fatalf("file property not escaped: %q", file)
	}
}

func TestWriteGitHubMessageIsCapped(t *testing.T) {
	files := make([]string, 500)
	for i := range files {
		files[i] = strings.Repeat("f", 100)
	}
	out := githubOf(t, []detect.Drift{{Doc: "a.md", Section: "S", Line: 1, Files: files}})
	if len(out) > 2500 {
		t.Fatalf("annotation too long: %d bytes", len(out))
	}
}
