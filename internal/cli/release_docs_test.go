package cli_test

import (
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/cli"
	"github.com/JtheGunner/doc-drift-checker/internal/testutil"
)

// releaseDocsReadme links one section to the release documents that the default
// ignore list covers and to one file that it must not cover (NOTES.md).
const releaseDocsReadme = "# Project\n\n## Usage\n\nRun `src/lib.go` to start.\n\n## Project info\n\n<!-- drift: CHANGELOG.md, LICENSE, LICENSE.md, LICENSE.txt, CONTRIBUTING.md, docs/CHANGELOG.md, NOTES.md -->\n\nWhere to find the history, the license and the rules.\n"

// releaseDocFiles are the paths that must never cause drift by default.
var releaseDocFiles = []string{
	"CHANGELOG.md",
	"LICENSE",
	"LICENSE.md",
	"LICENSE.txt",
	"CONTRIBUTING.md",
	"docs/CHANGELOG.md",
}

// releaseDocsRepo commits the base state on main. With branch it switches to a
// feature branch (diff mode); without it the repo stays on main (history mode).
func releaseDocsRepo(t *testing.T, branch bool) *testutil.Repo {
	t.Helper()
	r := testutil.NewRepo(t)
	r.Write("README.md", releaseDocsReadme)
	r.Write("src/lib.go", "package src\n\nfunc Lib() {}\n")
	r.Write("NOTES.md", "notes\n")
	for _, path := range releaseDocFiles {
		r.Write(path, "base\n")
	}
	r.Commit("base")
	if branch {
		r.Branch("feature")
	}
	t.Chdir(r.Dir)
	return r
}

func TestCheckReleaseDocsDoNotDriftByDefault(t *testing.T) {
	for _, path := range releaseDocFiles {
		t.Run(path, func(t *testing.T) {
			r := releaseDocsRepo(t, true)
			r.CommitFile(path, "changed\n", "change release document")
			expectSections(t, checkJSON(t, 0))
		})
	}

	t.Run("all at once", func(t *testing.T) {
		r := releaseDocsRepo(t, true)
		for _, path := range releaseDocFiles {
			r.Write(path, "changed\n")
		}
		r.Commit("change all release documents")
		expectSections(t, checkJSON(t, 0))
	})
}

func TestCheckReleaseDocsStillAllowRealDrift(t *testing.T) {
	t.Run("code changed next to a changelog entry", func(t *testing.T) {
		r := releaseDocsRepo(t, true)
		r.Write("CHANGELOG.md", "changed\n")
		r.Write("src/lib.go", "package src\n\nfunc New() {}\n")
		r.Commit("code and changelog")
		expectSections(t, checkJSON(t, cli.ExitDrift), "Usage")
	})

	t.Run("a linked file that is not a release document", func(t *testing.T) {
		r := releaseDocsRepo(t, true)
		r.CommitFile("NOTES.md", "changed\n", "change notes")
		expectSections(t, checkJSON(t, cli.ExitDrift), "Project info")
	})

	t.Run("a source file named like a release document", func(t *testing.T) {
		r := releaseDocsRepo(t, true)
		r.CommitFile("src/changelog.go", "package src\n", "add code")
		expectSections(t, checkJSON(t, 0))
	})
}

func TestHistoryReleaseDocsDoNotDriftByDefault(t *testing.T) {
	for _, path := range releaseDocFiles {
		t.Run(path, func(t *testing.T) {
			r := releaseDocsRepo(t, false)
			r.CommitFile(path, "changed\n", "change release document")
			if rep := historyJSON(t, 0); len(rep.Drifts) != 0 {
				t.Fatalf("drifts = %+v, want none", rep.Drifts)
			}
		})
	}

	t.Run("a linked file that is not a release document", func(t *testing.T) {
		r := releaseDocsRepo(t, false)
		r.CommitFile("NOTES.md", "changed\n", "change notes")
		rep := historyJSON(t, cli.ExitDrift)
		expectSections(t, rep.Drifts, "Project info")
	})
}
