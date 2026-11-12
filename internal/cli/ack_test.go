package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/cli"
	"github.com/JtheGunner/doc-drift-checker/internal/detect"
	"github.com/JtheGunner/doc-drift-checker/internal/lock"
	"github.com/JtheGunner/doc-drift-checker/internal/testutil"
)

// runAck runs the ack command and returns stdout, stderr and the exit code.
func runAck(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd := cli.NewRootCmd("dev")
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(append([]string{"ack"}, args...))
	err := cmd.Execute()
	return out.String(), errOut.String(), cli.ExitCode(err)
}

// mustAck runs ack and fails unless it succeeds.
func mustAck(t *testing.T, args ...string) string {
	t.Helper()
	out, errOut, code := runAck(t, args...)
	if code != 0 {
		t.Fatalf("ack %v: exit code %d\nstdout: %s\nstderr: %s", args, code, out, errOut)
	}
	return out
}

func assertNoLock(t *testing.T, r *testutil.Repo) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(r.Dir, lock.FileName)); err == nil {
		t.Fatal("lockfile was created")
	}
}

// ackError runs ack expecting exit code 2 and returns the error text.
func ackError(t *testing.T, args ...string) string {
	t.Helper()
	cmd := cli.NewRootCmd("dev")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(append([]string{"ack"}, args...))
	err := cmd.Execute()
	if cli.ExitCode(err) != cli.ExitFailure || err == nil {
		t.Fatalf("ack %v: err = %v, want exit code %d", args, err, cli.ExitFailure)
	}
	return err.Error()
}

func readLock(t *testing.T, r *testutil.Repo) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(r.Dir, lock.FileName))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestAckUnknownSectionFails(t *testing.T) {
	r := historyRepo(t)

	for _, section := range []string{"nope", "README.md#nope", "#nope", "OTHER.md#usage"} {
		if msg := ackError(t, section); !strings.Contains(msg, "unknown section") {
			t.Errorf("ack %q: error = %q", section, msg)
		}
	}
	assertNoLock(t, r)
}

func TestAckRequiresExactlyOneArgument(t *testing.T) {
	r := historyRepo(t)

	for _, args := range [][]string{{}, {"usage", "flags"}} {
		if _, _, code := runAck(t, args...); code != cli.ExitFailure {
			t.Errorf("ack %v: exit code = %d, want %d", args, code, cli.ExitFailure)
		}
	}
	assertNoLock(t, r)
}

func TestAckAcceptsFullKeyAndUniqueAnchor(t *testing.T) {
	for _, arg := range []string{"README.md#flags", "flags", "#flags"} {
		t.Run(arg, func(t *testing.T) {
			r := historyRepo(t)

			out := mustAck(t, arg)

			if out != "acknowledged README.md#flags (1 file)\n" {
				t.Fatalf("stdout = %q", out)
			}
			want := `{
  "version": 1,
  "sections": {
    "README.md#flags": {
      "files": {
        "src/flags.go": "` + lock.Hash([]byte("package src\n")) + `"
      }
    }
  }
}
`
			if got := readLock(t, r); got != want {
				t.Fatalf("lockfile:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func TestAckAmbiguousBareAnchorFails(t *testing.T) {
	r := historyRepo(t)
	r.Write(".docdrift.yaml", "docs:\n  - README.md\n  - docs/*.md\n")
	r.CommitFile("docs/guide.md", "# Guide\n\n## Usage\n\nSee `src/lib.go`.\n", "second doc")

	msg := ackError(t, "usage")
	for _, want := range []string{"ambiguous", "README.md#usage", "docs/guide.md#usage"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q should contain %q", msg, want)
		}
	}
	assertNoLock(t, r)

	mustAck(t, "docs/guide.md#usage")
	if got := readLock(t, r); !strings.Contains(got, `"docs/guide.md#usage"`) || strings.Contains(got, "README.md#usage") {
		t.Fatalf("lockfile:\n%s", got)
	}
}

func TestAckSectionWithoutMappedFilesFails(t *testing.T) {
	r := historyRepo(t)

	if msg := ackError(t, "project"); !strings.Contains(msg, "nothing to acknowledge") {
		t.Fatalf("error = %q", msg)
	}
	assertNoLock(t, r)
}

func TestAckSectionWithOnlyIgnoredFilesFails(t *testing.T) {
	r := historyRepo(t)
	r.Write("README.md", checkReadme+"\n## Tests\n\nSee `src/lib_test.go`.\n")
	r.CommitFile("src/lib_test.go", "package src\n", "test file")

	if msg := ackError(t, "tests"); !strings.Contains(msg, "nothing to acknowledge") {
		t.Fatalf("error = %q", msg)
	}
	assertNoLock(t, r)
}

func TestAckRefusesUncommittedMappedFile(t *testing.T) {
	tests := []struct {
		name   string
		change func(r *testutil.Repo)
	}{
		{"modified in the working tree", func(r *testutil.Repo) { r.Write("src/lib.go", newLib) }},
		{"staged", func(r *testutil.Repo) {
			r.Write("src/lib.go", newLib)
			r.Git("add", "src/lib.go")
		}},
		{"deleted in the working tree", func(r *testutil.Repo) {
			if err := os.Remove(filepath.Join(r.Dir, "src/lib.go")); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := historyRepo(t)
			tt.change(r)

			if msg := ackError(t, "usage"); !strings.Contains(msg, "src/lib.go") || !strings.Contains(msg, "uncommitted") {
				t.Fatalf("error = %q", msg)
			}
			assertNoLock(t, r)
		})
	}
}

func TestAckIgnoresUncommittedUnmappedFiles(t *testing.T) {
	r := historyRepo(t)
	r.Write("other/o.go", newLib)

	mustAck(t, "usage")
	assertLockHas(t, r, "README.md#usage")
}

func TestAckRefusesMappedFileAboveSizeCap(t *testing.T) {
	r := historyRepo(t)
	r.CommitFile("src/lib.go", "package src\n// "+strings.Repeat("x", detect.MaxFileBytes), "huge")

	if msg := ackError(t, "usage"); !strings.Contains(msg, "src/lib.go") || !strings.Contains(msg, "too large") {
		t.Fatalf("error = %q", msg)
	}
	assertNoLock(t, r)
}

func TestAckWorksFromSubdirectory(t *testing.T) {
	r := historyRepo(t)
	t.Chdir(filepath.Join(r.Dir, "src"))

	mustAck(t, "usage")

	assertLockHas(t, r, "README.md#usage")
	if _, err := os.Lstat(filepath.Join(r.Dir, "src", lock.FileName)); err == nil {
		t.Fatal("lockfile was written to the subdirectory")
	}
}

func TestAckLockfileIsDeterministicAndReAckTouchesOnlyItsEntry(t *testing.T) {
	r := historyRepo(t)
	mustAck(t, "other")
	mustAck(t, "usage")
	mustAck(t, "flags")

	hash := func(s string) string { return lock.Hash([]byte(s)) }
	want := `{
  "version": 1,
  "sections": {
    "README.md#flags": {
      "files": {
        "src/flags.go": "` + hash("package src\n") + `"
      }
    },
    "README.md#other": {
      "files": {
        "other/o.go": "` + hash("package other\n") + `"
      }
    },
    "README.md#usage": {
      "files": {
        "src/lib.go": "` + hash("package src\n") + `"
      }
    }
  }
}
`
	first := readLock(t, r)
	if first != want {
		t.Fatalf("lockfile:\n%s\nwant:\n%s", first, want)
	}

	// Acking again without a change rewrites identical bytes, whatever order
	// the sections were acknowledged in.
	mustAck(t, "usage")
	mustAck(t, "other")
	if got := readLock(t, r); got != want {
		t.Fatalf("lockfile changed without a code change:\n%s", got)
	}

	r.CommitFile("src/lib.go", newLib, "change code")
	mustAck(t, "usage")

	wantAfter := strings.Replace(want, hash("package src\n")+`"
      }
    }
  }
}
`, hash(newLib)+`"
      }
    }
  }
}
`, 1)
	if wantAfter == want {
		t.Fatal("test bug: expectation unchanged")
	}
	got := readLock(t, r)
	if got != wantAfter {
		t.Fatalf("lockfile after re-ack:\n%s\nwant:\n%s", got, wantAfter)
	}
	if strings.Contains(got, "\r") || !strings.HasSuffix(got, "}\n") || strings.HasSuffix(got, "\n\n") {
		t.Fatalf("lockfile line endings are not LF with one trailing newline: %q", got[len(got)-4:])
	}
}

func TestAckLeavesNoTemporaryFiles(t *testing.T) {
	r := historyRepo(t)
	mustAck(t, "usage")

	entries, err := os.ReadDir(r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), "tmp") || (strings.HasPrefix(e.Name(), ".") && e.Name() != ".git") {
			t.Errorf("leftover file %q", e.Name())
		}
	}
}

func TestAckExcludesTheLockfileItself(t *testing.T) {
	r := historyRepo(t)
	r.Write("README.md", checkReadme+"\n## Everything\n\n<!-- drift: ** -->\n")
	r.Commit("map everything")
	mustAck(t, "everything")
	r.Commit("commit lockfile")

	mustAck(t, "everything")

	if strings.Contains(readLock(t, r), `"doc-drift.lock"`) {
		t.Fatal("the lockfile hashes itself")
	}
}

func TestAckRejectsBadLockfiles(t *testing.T) {
	for name, content := range badLockfiles() {
		t.Run(name, func(t *testing.T) {
			r := historyRepo(t)
			r.Write(lock.FileName, content)

			if msg := ackError(t, "usage"); !strings.Contains(msg, lock.FileName) {
				t.Fatalf("error %q should name the lockfile", msg)
			}
			if readLock(t, r) != content {
				t.Fatal("bad lockfile was overwritten")
			}
		})
	}
}

func TestAckRejectsSymlinkedLockfile(t *testing.T) {
	r := historyRepo(t)
	target := filepath.Join(t.TempDir(), "elsewhere.lock")
	if err := os.WriteFile(target, []byte(`{"version":1,"sections":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(r.Dir, lock.FileName)); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	if msg := ackError(t, "usage"); !strings.Contains(msg, lock.FileName) {
		t.Fatalf("error %q should name the lockfile", msg)
	}
	if got, _ := os.ReadFile(target); string(got) != `{"version":1,"sections":{}}` {
		t.Fatalf("symlink target was modified: %q", got)
	}
}

func assertLockHas(t *testing.T, r *testutil.Repo, key string) {
	t.Helper()
	if !strings.Contains(readLock(t, r), `"`+key+`"`) {
		t.Fatalf("lockfile has no entry %q:\n%s", key, readLock(t, r))
	}
}

// badLockfiles are lockfile contents that every command must reject.
func badLockfiles() map[string]string {
	h := lock.Hash([]byte("x"))
	return map[string]string{
		"garbage":           "this is not json",
		"empty":             "",
		"wrong version":     `{"version":2,"sections":{}}`,
		"unknown field":     `{"version":1,"sections":{},"extra":true}`,
		"traversal path":    `{"version":1,"sections":{"README.md#usage":{"files":{"../../etc/passwd":"` + h + `"}}}}`,
		"absolute path":     `{"version":1,"sections":{"README.md#usage":{"files":{"/etc/passwd":"` + h + `"}}}}`,
		"duplicate section": `{"version":1,"sections":{"README.md#usage":{"files":{"a.go":"` + h + `"}},"README.md#usage":{"files":{"a.go":"` + h + `"}}}}`,
		"bad hash":          `{"version":1,"sections":{"README.md#usage":{"files":{"a.go":"nope"}}}}`,
		"trailing data":     `{"version":1,"sections":{}}{}`,
		"oversized":         `{"version":1,"sections":{}}` + strings.Repeat(" ", lock.MaxBytes),
	}
}
