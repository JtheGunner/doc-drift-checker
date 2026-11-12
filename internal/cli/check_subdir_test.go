package cli_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/cli"
)

func TestCheckDiffModeWorksFromASubdirectory(t *testing.T) {
	r := checkRepo(t)
	r.Write("src/lib.go", newLib)
	r.Write("README.md", editUsage(checkReadme))
	r.Commit("code and docs")
	r.CommitFile("src/flags.go", newFlags, "flags code")
	atRoot := checkJSON(t, cli.ExitDrift)

	if err := os.Chdir(filepath.Join(r.Dir, "src")); err != nil {
		t.Fatal(err)
	}
	if fromSub := checkJSON(t, cli.ExitDrift); !reflect.DeepEqual(fromSub, atRoot) {
		t.Fatalf("check from a subdirectory = %+v, at the root %+v", fromSub, atRoot)
	}
}
