package cli_test

import (
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/cli"
	"github.com/JtheGunner/doc-drift-checker/internal/testutil"
)

// The empty ignore list keeps the default ignore of release documents out of
// these tests: they pin the doc-to-doc rule, not the ignore defaults.
const docLinksConfig = "docs:\n  - README.md\n  - CHANGELOG.md\nignore: []\n"

const docLinksReadme = "# Project\n\n## Quick start\n\nSee `README.md` for details.\n\n## Changelog\n\nSee [changes](CHANGELOG.md).\n\n## Explicit\n\nText.\n\n<!-- drift: CHANGELOG.md -->\n"

const docLinksChangelog = "# Changelog\n\nChanged.\n"

// docLinksRepo commits a base state with doc-to-doc references on main and,
// when branch is set, switches to a feature branch.
func docLinksRepo(t *testing.T, branch bool) *testutil.Repo {
	t.Helper()
	r := testutil.NewRepo(t)
	r.Write(".docdrift.yaml", docLinksConfig)
	r.Write("README.md", docLinksReadme)
	r.Write("CHANGELOG.md", "# Changelog\n\nInitial.\n")
	r.Commit("base")
	if branch {
		r.Branch("feature")
	}
	t.Chdir(r.Dir)
	return r
}

func TestCheckDiffImplicitDocReferencesAreNotReported(t *testing.T) {
	r := docLinksRepo(t, true)
	r.CommitFile("CHANGELOG.md", docLinksChangelog, "update changelog")

	// Only the explicit annotation still links the changelog.
	expectSections(t, checkJSON(t, cli.ExitDrift), "Explicit")
}

func TestCheckHistoryImplicitDocReferencesAreNotReported(t *testing.T) {
	r := docLinksRepo(t, false)
	r.CommitFile("CHANGELOG.md", docLinksChangelog, "update changelog")

	expectSections(t, historyJSON(t, cli.ExitDrift).Drifts, "Explicit")
}
