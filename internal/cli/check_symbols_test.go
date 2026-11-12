package cli_test

import (
	"strings"
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/cli"
	"github.com/JtheGunner/doc-drift-checker/internal/testutil"
)

const symbolsReadme = "# Project\n\n" +
	"## Library\n\nSee `src/lib.go`.\n\n" +
	"## Flags\n\nSee `src/flags.go`.\n\n" +
	"## Tool\n\nRun `scripts/tool.py`.\n\n" +
	"## Private\n\nSee `src/private.go`.\n\n" +
	"## Broken\n\nSee `src/broken.go`.\n"

const libBase = "package src\n\n" +
	"import \"fmt\"\n\n" +
	"// Run starts the library.\n" +
	"func Run(name string) string {\n\treturn fmt.Sprint(name)\n}\n\n" +
	"func helper() int { return 1 }\n"

const flagsBase = "package src\n\ntype Flags struct {\n\tVerbose bool\n\tdebug   bool\n}\n"

// symbolsRepo commits a base with Go and Python files on main and switches to a feature branch.
func symbolsRepo(t *testing.T) *testutil.Repo {
	t.Helper()
	r := testutil.NewRepo(t)
	r.Write("README.md", symbolsReadme)
	r.Write("src/lib.go", libBase)
	r.Write("src/flags.go", flagsBase)
	r.Write("src/broken.go", "package src\n\nfunc Broken() {}\n")
	r.Write("src/private.go", "package src\n\nfunc hidden() {}\n")
	r.Write("scripts/tool.py", "def run():\n    return 1\n")
	r.Commit("base")
	r.Branch("feature")
	t.Chdir(r.Dir)
	return r
}

func TestCheckGoChangesThatAreNotAPIChanges(t *testing.T) {
	tests := []struct {
		name, path, content string
	}{
		{"body only", "src/lib.go", strings.Replace(libBase, "fmt.Sprint(name)", "fmt.Sprint(name, name)", 1)},
		{"comment only", "src/lib.go", strings.Replace(libBase, "// Run starts the library.", "// Run does something else now.", 1)},
		{"unexported function changed", "src/lib.go", strings.Replace(libBase, "return 1", "return 2", 1)},
		{"unexported function added", "src/lib.go", libBase + "\nfunc other() {}\n"},
		{"unexported struct field added", "src/flags.go", strings.Replace(flagsBase, "debug   bool", "debug   bool\n\ttrace   bool", 1)},
		{"import only", "src/lib.go", strings.Replace(libBase, "import \"fmt\"", "import \"fmt\"\nimport _ \"os\"", 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := symbolsRepo(t)
			r.CommitFile(tt.path, tt.content, "non-API change")
			expectSections(t, checkJSON(t, 0))
		})
	}
}

func TestCheckGoAPIChangesDrift(t *testing.T) {
	tests := []struct {
		name, path, content, section string
	}{
		{"signature changed", "src/lib.go", strings.Replace(libBase, "Run(name string)", "Run(name string, n int)", 1), "Library"},
		{"exported function added", "src/lib.go", libBase + "\nfunc Stop() {}\n", "Library"},
		{"exported struct field added", "src/flags.go", strings.Replace(flagsBase, "debug   bool", "debug   bool\n\tQuiet   bool", 1), "Flags"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := symbolsRepo(t)
			r.CommitFile(tt.path, tt.content, "API change")
			expectSections(t, checkJSON(t, cli.ExitDrift), tt.section)
		})
	}
}

func TestCheckUnsupportedLanguageFallsBackToPathRelevance(t *testing.T) {
	r := symbolsRepo(t)
	r.CommitFile("scripts/tool.py", "def run():\n    return 2\n", "python body change")
	drifts := checkJSON(t, cli.ExitDrift)
	expectSections(t, drifts, "Tool")
}

func TestCheckMixedChangeListsOnlyAPIChangedFile(t *testing.T) {
	r := symbolsRepo(t)
	r.Write("src/lib.go", strings.Replace(libBase, "fmt.Sprint(name)", "fmt.Sprint(name, name)", 1))
	r.Write("src/flags.go", flagsBase+"\nfunc NewFlags() Flags { return Flags{} }\n")
	r.Commit("body change and API change")

	drifts := checkJSON(t, cli.ExitDrift)
	expectSections(t, drifts, "Flags")
	if got := strings.Join(drifts[0].Files, ","); got != "src/flags.go" {
		t.Fatalf("files = %q, want only src/flags.go", got)
	}
}

func TestCheckAddedGoFile(t *testing.T) {
	tests := []struct {
		name, content string
		wantCode      int
		wantSections  []string
	}{
		{"only unexported symbols", "package src\n\nfunc internalOnly() {}\n", 0, nil},
		{"exported symbol", "package src\n\nfunc Public() {}\n", cli.ExitDrift, []string{"Library"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := symbolsRepo(t)
			r.Write(".docdrift.yaml", "mappings:\n  Library: [\"src/new.go\"]\n")
			r.Write("src/new.go", tt.content)
			r.Commit("add file")
			expectSections(t, checkJSON(t, tt.wantCode), tt.wantSections...)
		})
	}
}

func TestCheckDeletedGoFile(t *testing.T) {
	t.Run("with exported symbols", func(t *testing.T) {
		r := symbolsRepo(t)
		r.Remove("src/lib.go")
		r.Commit("delete lib")
		expectSections(t, checkJSON(t, cli.ExitDrift), "Library")
	})
	t.Run("with unexported symbols only", func(t *testing.T) {
		r := symbolsRepo(t)
		r.Remove("src/private.go")
		r.Commit("delete private")
		expectSections(t, checkJSON(t, 0))
	})
}

func TestCheckRenamedGoFileStaysRelevant(t *testing.T) {
	r := symbolsRepo(t)
	r.Move("src/lib.go", "src/library.go")
	r.Commit("rename")
	expectSections(t, checkJSON(t, cli.ExitDrift), "Library")
}

func TestCheckBrokenGoSyntaxDrifts(t *testing.T) {
	r := symbolsRepo(t)
	r.CommitFile("src/broken.go", "package src\n\nfunc Broken( {\n", "syntax error")
	expectSections(t, checkJSON(t, cli.ExitDrift), "Broken")
}

func TestCheckBrokenGoBaseDrifts(t *testing.T) {
	r := testutil.NewRepo(t)
	r.Write("README.md", symbolsReadme)
	r.Write("src/broken.go", "package src\n\nfunc Broken( {\n")
	r.Commit("base with broken file")
	r.Branch("feature")
	t.Chdir(r.Dir)
	r.CommitFile("src/broken.go", "package src\n\nfunc Broken() {}\n", "repair")
	expectSections(t, checkJSON(t, cli.ExitDrift), "Broken")
}

func TestCheckWhitespaceOnlyGoChangeIsStillDropped(t *testing.T) {
	r := symbolsRepo(t)
	r.CommitFile("src/lib.go", strings.Replace(libBase, "func Run(name string)", "func Run( name string )", 1)+"\n\n", "reformat")
	expectSections(t, checkJSON(t, 0))
}

func TestCheckAPIChangeOnBaseBranchAfterForkIsIgnored(t *testing.T) {
	r := symbolsRepo(t)
	r.Git("checkout", "--quiet", "main")
	r.CommitFile("src/lib.go", libBase+"\nfunc OnMain() {}\n", "main API change")
	r.Git("checkout", "--quiet", "feature")
	r.CommitFile("src/lib.go", strings.Replace(libBase, "fmt.Sprint(name)", "fmt.Sprint(name, name)", 1), "feature body change")
	// The old side is the merge base, not the tip of main.
	expectSections(t, checkJSON(t, 0))
}
