package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/cli"
	"github.com/JtheGunner/doc-drift-checker/internal/config"
	"github.com/JtheGunner/doc-drift-checker/internal/links"
	"github.com/JtheGunner/doc-drift-checker/internal/testutil"
)

const initReadme = "# Tool\n\nIntro without references.\n\n## Install\n\nBuild `cmd/tool/main.go` first.\n\n## Config\n\nSee `src/config.go`\nand `src/flags.go`.\n\n## Explicit\n\nSee `src/config.go`.\n<!-- drift: src/flags.go -->\n\n## Nothing\n\nNo files here.\n"

// initRepo commits a README with annotatable sections and returns the repo; the
// working directory is the repo root.
func initRepo(t *testing.T, readme string) *testutil.Repo {
	t.Helper()
	r := testutil.NewRepo(t)
	r.Write("README.md", readme)
	r.Write("cmd/tool/main.go", "package main\n")
	r.Write("src/config.go", "package src\n")
	r.Write("src/flags.go", "package src\n")
	r.Commit("base")
	t.Chdir(r.Dir)
	return r
}

func runInitCmd(t *testing.T, args ...string) (stdout string, err error) {
	t.Helper()
	var out bytes.Buffer
	cmd := cli.NewRootCmd("dev")
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(append([]string{"init"}, args...))
	err = cmd.Execute()
	return out.String(), err
}

func mustInit(t *testing.T, args ...string) string {
	t.Helper()
	out, err := runInitCmd(t, args...)
	if err != nil {
		t.Fatalf("init %v: %v\n%s", args, err, out)
	}
	return out
}

func initFails(t *testing.T, args ...string) string {
	t.Helper()
	_, err := runInitCmd(t, args...)
	if err == nil || cli.ExitCode(err) != cli.ExitFailure {
		t.Fatalf("init %v: err = %v, want exit code %d", args, err, cli.ExitFailure)
	}
	return err.Error()
}

func readRepoFile(t *testing.T, r *testutil.Repo, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(r.Dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestInitWritesValidStarterConfig(t *testing.T) {
	r := initRepo(t, initReadme)

	out := mustInit(t)

	if got := readRepoFile(t, r, config.FileName); got != string(config.Starter()) {
		t.Errorf("config differs from the starter:\n%s", got)
	}
	if _, err := config.Load(r.Dir); err != nil {
		t.Errorf("config.Load of the written file: %v", err)
	}
	if !strings.Contains(out, "wrote "+config.FileName) {
		t.Errorf("output lacks the written file:\n%s", out)
	}
}

func TestInitPrintsSuggestionsAndLeavesReadmeUntouched(t *testing.T) {
	r := initRepo(t, initReadme)

	out := mustInit(t)

	for _, want := range []string{
		"README.md",
		"Install",
		"<!-- drift: cmd/tool/main.go -->",
		"<!-- drift: src/config.go, src/flags.go -->",
		"2 annotation suggestion(s), not written",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Explicit") || strings.Contains(out, "Nothing") {
		t.Errorf("sections that must not be suggested were printed:\n%s", out)
	}
	if got := readRepoFile(t, r, "README.md"); got != initReadme {
		t.Errorf("README changed without --write:\n%s", got)
	}
}

func TestInitWithoutSuggestionsSaysSo(t *testing.T) {
	initRepo(t, "# Tool\n\nNothing to link.\n")

	if out := mustInit(t); !strings.Contains(out, "no annotation suggestions") {
		t.Errorf("output = %q", out)
	}
}

func TestInitDoesNotOverwriteExistingConfig(t *testing.T) {
	r := initRepo(t, initReadme)
	r.Write(config.FileName, "docs: [\"OTHER.md\"]\n")

	msg := initFails(t, "--write")

	if !strings.Contains(msg, "--force") {
		t.Errorf("error = %q, want a hint at --force", msg)
	}
	if got := readRepoFile(t, r, config.FileName); got != "docs: [\"OTHER.md\"]\n" {
		t.Errorf("config was changed: %q", got)
	}
	if got := readRepoFile(t, r, "README.md"); got != initReadme {
		t.Error("README was changed although init failed")
	}
}

func TestInitForceReplacesInvalidConfig(t *testing.T) {
	r := initRepo(t, initReadme)
	r.Write(config.FileName, "bogus-key: [\n")

	mustInit(t, "--force")

	if got := readRepoFile(t, r, config.FileName); got != string(config.Starter()) {
		t.Errorf("config was not replaced:\n%s", got)
	}
}

func TestInitRefusesNonRegularConfigTarget(t *testing.T) {
	for _, tt := range []struct {
		name  string
		setup func(t *testing.T, r *testutil.Repo, outside string)
	}{
		{"symlink", func(t *testing.T, r *testutil.Repo, outside string) {
			if err := os.Symlink(outside, filepath.Join(r.Dir, config.FileName)); err != nil {
				t.Fatal(err)
			}
		}},
		{"directory", func(t *testing.T, r *testutil.Repo, _ string) {
			if err := os.Mkdir(filepath.Join(r.Dir, config.FileName), 0o755); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		for _, args := range [][]string{{}, {"--force"}} {
			t.Run(tt.name+" "+strings.Join(args, " "), func(t *testing.T) {
				r := initRepo(t, initReadme)
				outside := filepath.Join(t.TempDir(), "victim.yaml")
				if err := os.WriteFile(outside, []byte("keep\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				tt.setup(t, r, outside)

				initFails(t, append([]string{"--write"}, args...)...)

				if data, _ := os.ReadFile(outside); string(data) != "keep\n" {
					t.Errorf("symlink target was written: %q", data)
				}
				if got := readRepoFile(t, r, "README.md"); got != initReadme {
					t.Error("README was changed although init failed")
				}
			})
		}
	}
}

func TestInitWriteInsertsAnnotationsAndRoundTrips(t *testing.T) {
	r := initRepo(t, initReadme)

	out := mustInit(t, "--write")

	want := strings.Replace(initReadme, "## Install\n", "## Install\n<!-- drift: cmd/tool/main.go -->\n", 1)
	want = strings.Replace(want, "## Config\n", "## Config\n<!-- drift: src/config.go, src/flags.go -->\n", 1)
	if got := readRepoFile(t, r, "README.md"); got != want {
		t.Errorf("README = %q\nwant %q", got, want)
	}
	if !strings.Contains(out, "2 annotation suggestion(s), 2 written") {
		t.Errorf("output:\n%s", out)
	}

	report, err := buildLinksJSON(t)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]string{}
	for _, s := range report.Docs[0].Sections {
		for _, l := range s.Links {
			if s.Heading == "Install" || s.Heading == "Config" {
				if l.Origin != links.OriginAnnotation {
					t.Errorf("%s: %s has origin %s", s.Heading, l.File, l.Origin)
				}
				got[s.Heading] = append(got[s.Heading], l.File)
			}
		}
	}
	if strings.Join(got["Install"], ",") != "cmd/tool/main.go" || strings.Join(got["Config"], ",") != "src/config.go,src/flags.go" {
		t.Errorf("round-trip files = %v", got)
	}
}

func buildLinksJSON(t *testing.T) (links.Report, error) {
	t.Helper()
	var out bytes.Buffer
	cmd := cli.NewRootCmd("dev")
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"links", "--json"})
	if err := cmd.Execute(); err != nil {
		return links.Report{}, err
	}
	var report links.Report
	err := json.Unmarshal(out.Bytes(), &report)
	return report, err
}

func TestInitWriteIsIdempotent(t *testing.T) {
	r := initRepo(t, initReadme)
	mustInit(t, "--write")
	r.Commit("annotate")
	after := readRepoFile(t, r, "README.md")

	out := mustInit(t, "--force", "--write")

	if !strings.Contains(out, "no annotation suggestions") {
		t.Errorf("second run suggested again:\n%s", out)
	}
	if got := readRepoFile(t, r, "README.md"); got != after {
		t.Error("second run changed the README")
	}
}

func TestInitWriteHeadingKinds(t *testing.T) {
	readme := "Install\n=======\n\nBuild `cmd/tool/main.go`.\n\nConfig\n------\n\nSee `src/config.go`.\n"
	r := initRepo(t, readme)

	mustInit(t, "--write")

	want := "Install\n=======\n<!-- drift: cmd/tool/main.go -->\n\nBuild `cmd/tool/main.go`.\n\nConfig\n------\n<!-- drift: src/config.go -->\n\nSee `src/config.go`.\n"
	if got := readRepoFile(t, r, "README.md"); got != want {
		t.Errorf("README = %q\nwant %q", got, want)
	}
}

func TestInitWriteKeepsCRLFAndMode(t *testing.T) {
	crlf := strings.ReplaceAll(initReadme, "\n", "\r\n")
	r := initRepo(t, crlf)
	if err := os.Chmod(filepath.Join(r.Dir, "README.md"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.Git("config", "core.fileMode", "false")

	mustInit(t, "--write")

	want := strings.Replace(crlf, "## Install\r\n", "## Install\r\n<!-- drift: cmd/tool/main.go -->\r\n", 1)
	want = strings.Replace(want, "## Config\r\n", "## Config\r\n<!-- drift: src/config.go, src/flags.go -->\r\n", 1)
	if got := readRepoFile(t, r, "README.md"); got != want {
		t.Errorf("README = %q\nwant %q", got, want)
	}
	info, err := os.Stat(filepath.Join(r.Dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestInitWriteRefusesDirtyReadme(t *testing.T) {
	r := initRepo(t, initReadme)
	dirty := initReadme + "\nLocal edit.\n"
	r.Write("README.md", dirty)

	msg := initFails(t, "--write")

	if !strings.Contains(msg, "uncommitted") {
		t.Errorf("error = %q", msg)
	}
	if got := readRepoFile(t, r, "README.md"); got != dirty {
		t.Error("dirty README was changed")
	}
	if _, err := os.Lstat(filepath.Join(r.Dir, config.FileName)); err == nil {
		t.Error("config was written although the run was refused")
	}
}

func TestInitDirtyReadmeIsFineWithoutWrite(t *testing.T) {
	r := initRepo(t, initReadme)
	r.Write("README.md", initReadme+"\nLocal edit.\n")

	mustInit(t)
}

func TestInitDoesNotSuggestUnsafeFileNames(t *testing.T) {
	readme := "## Commas\n\nSee `src/a,b.go`.\n\n## Globs\n\nSee `src/[x].go`.\n\n## Marker\n\nSee `src/a-->b.go`.\n"
	r := testutil.NewRepo(t)
	r.Write("README.md", readme)
	r.Write("src/a,b.go", "package src\n")
	r.Write("src/[x].go", "package src\n")
	r.Write("src/a-->b.go", "package src\n")
	r.Commit("base")
	t.Chdir(r.Dir)

	out := mustInit(t, "--write")

	if got := readRepoFile(t, r, "README.md"); got != readme {
		t.Errorf("README changed:\n%s", got)
	}
	if strings.Contains(out, "<!-- drift:") {
		t.Errorf("output:\n%s", out)
	}
}

func TestInitDoesNotSuggestTooBroadSections(t *testing.T) {
	var refs []string
	r := testutil.NewRepo(t)
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k"} {
		r.Write("src/"+n+".go", "package src\n")
		refs = append(refs, "`src/"+n+".go`")
	}
	readme := "## Many\n\nSee " + strings.Join(refs, ", ") + ".\n"
	r.Write("README.md", readme)
	r.Commit("base")
	t.Chdir(r.Dir)

	out := mustInit(t, "--write")

	if !strings.Contains(out, "no annotation suggestions") {
		t.Errorf("output:\n%s", out)
	}
	if got := readRepoFile(t, r, "README.md"); got != readme {
		t.Error("README changed")
	}
}

func TestInitNeutralisesControlCharactersInHeadings(t *testing.T) {
	readme := "## Evil \x1b[31mred\x07 `src/config.go`\n\nText.\n"
	initRepo(t, readme)

	out := mustInit(t)

	if strings.ContainsAny(out, "\x1b\x07") {
		t.Errorf("control characters in output: %q", out)
	}
	if !strings.Contains(out, "Evil ?[31mred?") {
		t.Errorf("heading not shown neutralised: %q", out)
	}
}

func TestInitCapsLongHeadings(t *testing.T) {
	readme := "## " + strings.Repeat("x", 500) + " `src/config.go`\n\nText.\n"
	initRepo(t, readme)

	out := mustInit(t)

	if strings.Contains(out, strings.Repeat("x", 200)) {
		t.Error("long heading is not capped")
	}
}

func TestInitFromSubdirectory(t *testing.T) {
	r := initRepo(t, initReadme)
	t.Chdir(filepath.Join(r.Dir, "src"))

	mustInit(t, "--write")

	if _, err := os.Lstat(filepath.Join(r.Dir, config.FileName)); err != nil {
		t.Errorf("config not written in the repo root: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(r.Dir, "src", config.FileName)); err == nil {
		t.Error("config written in the subdirectory")
	}
	if !strings.Contains(readRepoFile(t, r, "README.md"), "<!-- drift: cmd/tool/main.go -->") {
		t.Error("README not annotated")
	}
}

func TestInitWithoutTrackedReadme(t *testing.T) {
	r := testutil.NewRepo(t)
	r.Write("main.go", "package main\n")
	r.Commit("base")
	t.Chdir(r.Dir)

	out := mustInit(t, "--write")

	if !strings.Contains(out, "no annotation suggestions") {
		t.Errorf("output:\n%s", out)
	}
	if _, err := os.Lstat(filepath.Join(r.Dir, config.FileName)); err != nil {
		t.Errorf("config not written: %v", err)
	}
}

func TestInitOutsideRepositoryFails(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	initFails(t)

	if _, err := os.Lstat(filepath.Join(dir, config.FileName)); err == nil {
		t.Error("config written outside a repository")
	}
}

func TestInitRejectsArguments(t *testing.T) {
	initRepo(t, initReadme)

	initFails(t, "extra")
}
