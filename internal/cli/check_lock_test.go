package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/cli"
	"github.com/JtheGunner/doc-drift-checker/internal/lock"
)

const (
	changedLib  = "package src\n\nfunc Changed() {}\n"
	changedLib2 = "package src\n\nfunc ChangedAgain() {}\n"
)

func filesOf(drifts []jsonDrift, section string) string {
	for _, d := range drifts {
		if d.Section == section {
			return strings.Join(d.Files, ",")
		}
	}
	return "<none>"
}

func TestHistoryLockSuppressesDriftUntilNextRelevantChange(t *testing.T) {
	r := historyRepo(t)
	r.CommitFile("src/lib.go", newLib, "change code")
	expectSections(t, historyJSON(t, cli.ExitDrift).Drifts, "Usage")

	mustAck(t, "usage")
	expectSections(t, historyJSON(t, 0).Drifts)

	r.CommitFile("src/lib.go", changedLib, "change code again")
	drifts := historyJSON(t, cli.ExitDrift).Drifts
	expectSections(t, drifts, "Usage")
	if got := filesOf(drifts, "Usage"); got != "src/lib.go" {
		t.Fatalf("files = %q", got)
	}

	// Re-acknowledging the new state clears the drift again.
	mustAck(t, "usage")
	expectSections(t, historyJSON(t, 0).Drifts)
}

func TestHistoryLockIgnoresChangesInIgnoredFiles(t *testing.T) {
	r := historyRepo(t)
	r.CommitFile("other/o_test.go", "package other\n", "add test")
	r.CommitFile("other/o.go", "package other\n\nfunc O() {}\n", "change code")
	expectSections(t, historyJSON(t, cli.ExitDrift).Drifts, "Other")
	mustAck(t, "other")

	r.CommitFile("other/o_test.go", "package other\n\nfunc TestO() {}\n", "change test")
	r.CommitFile("other/extra_test.go", "package other\n", "add another test")
	r.Remove("other/o_test.go")
	r.Commit("remove test")

	expectSections(t, historyJSON(t, 0).Drifts)
}

func TestHistoryLockAddedOrRemovedMappedFileIsDrift(t *testing.T) {
	r := historyRepo(t)
	r.CommitFile("other/p.go", "package other\n", "second file")
	mustAck(t, "other")
	expectSections(t, historyJSON(t, 0).Drifts)

	r.CommitFile("other/q.go", "package other\n", "added file")
	drifts := historyJSON(t, cli.ExitDrift).Drifts
	expectSections(t, drifts, "Other")
	if got := filesOf(drifts, "Other"); got != "other/q.go" {
		t.Fatalf("files = %q", got)
	}
	mustAck(t, "other")
	expectSections(t, historyJSON(t, 0).Drifts)

	r.Remove("other/p.go")
	r.Commit("removed file")
	drifts = historyJSON(t, cli.ExitDrift).Drifts
	expectSections(t, drifts, "Other")
	if got := filesOf(drifts, "Other"); got != "other/p.go" {
		t.Fatalf("files = %q", got)
	}
}

func TestHistoryLockRenamedAwayMappedFileIsDrift(t *testing.T) {
	r := historyRepo(t)
	mustAck(t, "usage")

	r.Move("src/lib.go", "src/library.go")
	r.Commit("rename")

	drifts := historyJSON(t, cli.ExitDrift).Drifts
	expectSections(t, drifts, "Usage")
	if got := filesOf(drifts, "Usage"); got != "src/lib.go" {
		t.Fatalf("files = %q", got)
	}
}

func TestHistoryLockEntryWinsOverNewerCode(t *testing.T) {
	r := historyRepo(t)
	r.CommitFile("src/lib.go", newLib, "code newer than docs")
	mustAck(t, "usage")
	r.Commit("record lockfile")

	// Blame alone would report Usage here; the matching lock state wins.
	expectSections(t, historyJSON(t, 0).Drifts)
}

func TestHistoryLockMismatchWinsOverNewerDocs(t *testing.T) {
	r := historyRepo(t)
	mustAck(t, "usage")
	r.CommitFile("src/lib.go", newLib, "change code")
	r.CommitFile("README.md", editUsage(checkReadme), "docs updated after the code")

	// Without a lock entry the newer docs would mean no drift.
	drifts := historyJSON(t, cli.ExitDrift).Drifts
	expectSections(t, drifts, "Usage")
	if got := filesOf(drifts, "Usage"); got != "src/lib.go" {
		t.Fatalf("files = %q", got)
	}

	mustAck(t, "usage")
	expectSections(t, historyJSON(t, 0).Drifts)
}

func TestHistorySectionsWithoutEntryKeepNormalLogic(t *testing.T) {
	r := historyRepo(t)
	mustAck(t, "usage")
	r.CommitFile("src/flags.go", newFlags, "change flags code")

	expectSections(t, historyJSON(t, cli.ExitDrift).Drifts, "Flags")
}

func TestHistoryLockedSectionOfAnotherSectionIsNotAffected(t *testing.T) {
	r := historyRepo(t)
	r.CommitFile("src/lib.go", newLib, "change lib")
	r.CommitFile("src/flags.go", newFlags, "change flags")
	mustAck(t, "usage")

	expectSections(t, historyJSON(t, cli.ExitDrift).Drifts, "Flags")
}

func TestDiffLockSuppressesOnlyMatchingState(t *testing.T) {
	r := checkRepo(t)
	r.CommitFile("src/lib.go", "package src\n\nfunc New() {}\n", "change lib")
	r.CommitFile("other/o.go", "package other\n\nfunc O() {}\n", "change other")
	expectSections(t, checkJSON(t, cli.ExitDrift), "Usage", "Other")

	mustAck(t, "usage")
	expectSections(t, checkJSON(t, cli.ExitDrift), "Other")

	mustAck(t, "other")
	expectSections(t, checkJSON(t, 0))

	r.CommitFile("src/lib.go", changedLib2, "change lib again")
	drifts := checkJSON(t, cli.ExitDrift)
	expectSections(t, drifts, "Usage")
	if got := filesOf(drifts, "Usage"); got != "src/lib.go" {
		t.Fatalf("files = %q", got)
	}
}

func TestDiffLockMismatchWithoutDriftReportsNothing(t *testing.T) {
	r := checkRepo(t)
	mustAck(t, "usage")
	r.Write("src/lib.go", changedLib)
	r.Write("README.md", editUsage(checkReadme))
	r.Commit("code and docs together")

	// Diff mode keeps its own verdict when the state does not match.
	expectSections(t, checkJSON(t, 0))
}

func TestDiffLockedDriftKeepsWarnOnlyAndSeverityBehaviour(t *testing.T) {
	r := checkRepo(t)
	r.CommitFile("src/lib.go", "package src\n\nfunc New() {}\n", "change lib")
	r.CommitFile("other/o.go", "package other\n\nfunc O() {}\n", "change other")
	mustAck(t, "usage")

	if _, _, code := runCheck(t, "--base", "main", "--warn-only"); code != 0 {
		t.Fatalf("--warn-only exit code = %d", code)
	}
	out, _, code := runCheck(t, "--base", "main")
	if code != cli.ExitDrift || strings.Contains(out, "Usage") || !strings.Contains(out, "Other") {
		t.Fatalf("exit code %d, output:\n%s", code, out)
	}
}

func TestCheckWithoutLockfileBehavesAsBefore(t *testing.T) {
	r := historyRepo(t)
	r.CommitFile("src/lib.go", newLib, "change code")

	expectSections(t, historyJSON(t, cli.ExitDrift).Drifts, "Usage")
	assertNoLock(t, r)
}

func TestCheckRejectsBadLockfiles(t *testing.T) {
	for name, content := range badLockfiles() {
		t.Run(name, func(t *testing.T) {
			r := historyRepo(t)
			r.Write(lock.FileName, content)

			for _, args := range [][]string{{}, {"--base", "HEAD"}} {
				out, errOut, code := runCheck(t, args...)
				if code != cli.ExitFailure {
					t.Fatalf("check %v: exit code = %d, want %d\nstdout: %s\nstderr: %s", args, code, cli.ExitFailure, out, errOut)
				}
			}
		})
	}
}

func TestCheckRejectsSymlinkedLockfile(t *testing.T) {
	r := historyRepo(t)
	target := filepath.Join(t.TempDir(), "elsewhere.lock")
	if err := os.WriteFile(target, []byte(`{"version":1,"sections":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(r.Dir, lock.FileName)); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	for _, args := range [][]string{{}, {"--base", "HEAD"}} {
		if _, _, code := runCheck(t, args...); code != cli.ExitFailure {
			t.Fatalf("check %v: exit code = %d, want %d", args, code, cli.ExitFailure)
		}
	}
}

func TestCheckIgnoresLockEntriesOfUnknownSections(t *testing.T) {
	r := historyRepo(t)
	h := lock.Hash([]byte("x"))
	r.Write(lock.FileName, `{"version":1,"sections":{"README.md#gone":{"files":{"a.go":"`+h+`"}}}}`)
	r.CommitFile("src/lib.go", newLib, "change code")

	expectSections(t, historyJSON(t, cli.ExitDrift).Drifts, "Usage")
}
