package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/cli"
	"github.com/JtheGunner/doc-drift-checker/internal/testutil"
)

const linksReadme = "# Project\n\n## Usage\n\n<!-- drift: cmd/**/*.go -->\n\n## Notes\n\nSee `gone.go` and `src/lib.go`.\n\n## Empty\n\nJust prose.\n"

func linksRepo(t *testing.T) *testutil.Repo {
	t.Helper()
	r := testutil.NewRepo(t)
	r.Write("README.md", linksReadme)
	r.Write("cmd/app/main.go", "package main\n")
	r.Write("src/lib.go", "package src\n")
	r.Commit("base")
	r.Write("cmd/untracked.go", "package main\n")
	t.Chdir(r.Dir)
	return r
}

func TestLinksCommandText(t *testing.T) {
	linksRepo(t)
	out, err := run(t, "dev", "links")
	if err != nil {
		t.Fatalf("links returned error: %v\n%s", err, out)
	}
	want := "README.md\n" +
		"  Project (lines 1-13)\n" +
		"    unmapped\n" +
		"  Usage (lines 3-5)\n" +
		"    cmd/app/main.go [annotation]\n" +
		"  Notes (lines 7-9)\n" +
		"    src/lib.go [implicit]\n" +
		"    missing: gone.go [implicit] (line 9)\n" +
		"  Empty (lines 11-13)\n" +
		"    unmapped\n"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
	if strings.Contains(out, "untracked.go") {
		t.Fatal("untracked files must not be linked")
	}
}

func TestLinksCommandJSON(t *testing.T) {
	linksRepo(t)
	out, err := run(t, "dev", "links", "--json")
	if err != nil {
		t.Fatalf("links --json returned error: %v", err)
	}
	var report struct {
		Docs []struct {
			Doc      string
			Sections []struct {
				Heading  string
				Unmapped bool
			}
		}
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(report.Docs) != 1 || report.Docs[0].Doc != "README.md" || len(report.Docs[0].Sections) != 4 {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestLinksCommandUsesConfigDocsAndMappings(t *testing.T) {
	r := linksRepo(t)
	r.CommitFile(".docdrift.yaml", "docs: [\"README.md\"]\nmappings:\n  Empty: [\"src/*.go\"]\n", "config")
	out, err := run(t, "dev", "links")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "src/lib.go [config]") {
		t.Fatalf("config mapping missing:\n%s", out)
	}
}

func TestLinksCommandSucceedsWhenAllSectionsUnmapped(t *testing.T) {
	r := testutil.NewRepo(t)
	r.CommitFile("README.md", "# T\n\nprose\n", "base")
	t.Chdir(r.Dir)
	if _, err := run(t, "dev", "links"); err != nil {
		t.Fatalf("unmapped sections must not be an error: %v", err)
	}
}

func TestLinksCommandOutsideGitRepoFails(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := run(t, "dev", "links"); err == nil {
		t.Fatal("expected an error outside a git repository")
	}
}

func TestLinksCommandInvalidConfigFails(t *testing.T) {
	r := testutil.NewRepo(t)
	r.CommitFile("README.md", "# T\n", "base")
	r.Write(".docdrift.yaml", "bogus: 1\n")
	t.Chdir(r.Dir)
	if _, err := run(t, "dev", "links"); err == nil {
		t.Fatal("expected an error for an invalid config")
	}
}

func TestLinksCommandRejectsSymlinkedDoc(t *testing.T) {
	outsideFile := filepath.Join(t.TempDir(), "secret.md")
	if err := os.WriteFile(outsideFile, []byte("# Secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(r *testutil.Repo) string{
		"outside the repo": func(*testutil.Repo) string { return outsideFile },
		"inside the repo": func(r *testutil.Repo) string {
			r.Write("docs/real.md", "# Real\n")
			return filepath.Join(r.Dir, "docs", "real.md")
		},
	}
	for name, target := range tests {
		t.Run(name, func(t *testing.T) {
			r := testutil.NewRepo(t)
			targetPath := target(r)
			if err := os.Symlink(targetPath, filepath.Join(r.Dir, "README.md")); err != nil {
				t.Skipf("symlinks unsupported: %v", err)
			}
			r.Commit("base")
			t.Chdir(r.Dir)
			_, err := run(t, "dev", "links")
			if err == nil || !strings.Contains(err.Error(), "README.md") || !strings.Contains(err.Error(), "regular file") {
				t.Fatalf("expected an error naming README.md, got %v", err)
			}
		})
	}
}

func TestLinksCommandReadsRegularDoc(t *testing.T) {
	linksRepo(t)
	if _, err := run(t, "dev", "links"); err != nil {
		t.Fatalf("regular doc file must still work: %v", err)
	}
}

func TestLinksCommandReportsUnmatchedMappingKeys(t *testing.T) {
	r := testutil.NewRepo(t)
	r.Write("README.md", "# Project\n\n## 🚩 Flags\n\ntext\n\n## Usage\n\ntext\n")
	r.Write("src/lib.go", "package src\n")
	r.Write(".docdrift.yaml", "mappings:\n  Flags: [\"src/*.go\"]\n  \"🚩 Flags\": [\"src/*.go\"]\n  \"#usage\": [\"src/*.go\"]\n")
	r.Commit("base")
	t.Chdir(r.Dir)

	out, err := run(t, "dev", "links")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out, "unmatched mapping: Flags [config]\n") || strings.Count(out, "unmatched mapping:") != 1 {
		t.Fatalf("expected exactly the key Flags to be reported:\n%s", out)
	}

	out, err = run(t, "dev", "links", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		UnmatchedMappings []struct{ Ref, Origin string }
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.UnmatchedMappings) != 1 || report.UnmatchedMappings[0].Ref != "Flags" || report.UnmatchedMappings[0].Origin != "config" {
		t.Fatalf("got %+v", report.UnmatchedMappings)
	}
}

func TestLinksCommandMatchingMappingsReportNothing(t *testing.T) {
	r := linksRepo(t)
	r.CommitFile(".docdrift.yaml", "mappings:\n  Empty: [\"src/*.go\"]\n  \"#notes\": [\"src/*.go\"]\n", "config")
	out, err := run(t, "dev", "links", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "\"unmatchedMappings\": []") {
		t.Fatalf("expected an empty list:\n%s", out)
	}
}

func TestUnmatchedMappingKeyDoesNotChangeCheckOrExplain(t *testing.T) {
	setup := func(config string) *testutil.Repo {
		r := testutil.NewRepo(t)
		r.Write("README.md", "# Project\n\n## Usage\n\nprose\n")
		r.Write("src/lib.go", "package src\n")
		r.Write(".docdrift.yaml", config)
		r.Commit("base")
		r.Write("src/lib.go", "package src\n\nfunc Added() {}\n")
		r.Commit("code change")
		t.Chdir(r.Dir)
		return r
	}
	const mapped = "mappings:\n  Usage: [\"src/*.go\"]\n"
	setup(mapped)
	wantCheck, wantErr := run(t, "dev", "check", "--base", "HEAD~1")
	wantExplain, wantExplainErr := run(t, "dev", "explain", "Usage", "--base", "HEAD~1")

	setup(mapped + "  Typo: [\"src/*.go\"]\n")
	gotCheck, gotErr := run(t, "dev", "check", "--base", "HEAD~1")
	gotExplain, gotExplainErr := run(t, "dev", "explain", "Usage", "--base", "HEAD~1")

	if gotCheck != wantCheck || cli.ExitCode(gotErr) != cli.ExitCode(wantErr) || wantErr == nil {
		t.Fatalf("check changed:\n%s\nvs\n%s (%v / %v)", gotCheck, wantCheck, gotErr, wantErr)
	}
	if gotExplain != wantExplain || (gotExplainErr == nil) != (wantExplainErr == nil) {
		t.Fatalf("explain changed:\n%s\nvs\n%s", gotExplain, wantExplain)
	}
}
