package cli_test

import (
	"encoding/json"
	"strings"
	"testing"

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
