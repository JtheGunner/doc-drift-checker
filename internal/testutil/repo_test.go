package testutil_test

import (
	"strings"
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/testutil"
)

func TestRepoIsDeterministic(t *testing.T) {
	build := func() (string, string) {
		r := testutil.NewRepo(t)
		first := r.CommitFile("a.txt", "one\n", "first")
		second := r.CommitFile("a.txt", "two\n", "second")
		return first, second
	}
	a1, a2 := build()
	b1, b2 := build()
	if a1 != b1 || a2 != b2 {
		t.Fatalf("commit SHAs differ between identical builds: %s/%s vs %s/%s", a1, a2, b1, b2)
	}
}

func TestRepoHasFixedIdentityAndDates(t *testing.T) {
	r := testutil.NewRepo(t)
	r.CommitFile("a.txt", "one\n", "first")
	r.CommitFile("a.txt", "two\n", "second")
	got := r.Git("log", "--reverse", "--format=%an <%ae> %aI")
	want := "Test Author <test@example.com> 2024-01-01T12:01:00Z\n" +
		"Test Author <test@example.com> 2024-01-01T12:02:00Z"
	if got != want {
		t.Fatalf("log mismatch:\n%s\nwant:\n%s", got, want)
	}
}

func TestRepoHasNoRemote(t *testing.T) {
	r := testutil.NewRepo(t)
	if remotes := r.Git("remote"); remotes != "" {
		t.Fatalf("expected no remotes, got %q", remotes)
	}
}

func TestShallowCloneIsShallowWithoutRemote(t *testing.T) {
	r := testutil.NewRepo(t)
	r.CommitFile("a.txt", "1\n", "one")
	r.CommitFile("a.txt", "2\n", "two")
	clone := r.ShallowClone(1)
	if got := clone.Git("rev-parse", "--is-shallow-repository"); got != "true" {
		t.Fatalf("expected shallow clone, got %q", got)
	}
	if remotes := clone.Git("remote"); strings.TrimSpace(remotes) != "" {
		t.Fatalf("expected no remotes, got %q", remotes)
	}
}
