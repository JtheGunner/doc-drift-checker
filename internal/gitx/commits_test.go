package gitx_test

import (
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/gitx"
	"github.com/JtheGunner/doc-drift-checker/internal/testutil"
)

func TestCommitsBetweenListsNewestFirstWithinBaseToHead(t *testing.T) {
	r := testutil.NewRepo(t)
	r.CommitFile("a.txt", "0\n", "base")
	r.Branch("feature")
	first := r.CommitFile("a.txt", "1\n", "first change")
	r.CommitFile("other.txt", "x\n", "unrelated")
	second := r.CommitFile("a.txt", "2\n", "second change")

	commits, err := gitx.New(r.Dir).CommitsBetween("main", []string{"a.txt"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 {
		t.Fatalf("commits = %+v, want 2", commits)
	}
	if commits[0].Hash != second || commits[0].Subject != "second change" || commits[0].Time != commitTime(4) {
		t.Errorf("newest = %+v", commits[0])
	}
	if commits[1].Hash != first || commits[1].Subject != "first change" {
		t.Errorf("oldest = %+v", commits[1])
	}
}

func TestCommitsBetweenCoversBothPathsOfARename(t *testing.T) {
	r := testutil.NewRepo(t)
	r.CommitFile("old.txt", "0\n", "base")
	r.Branch("feature")
	edit := r.CommitFile("old.txt", "1\n", "edit before rename")
	r.Move("old.txt", "new.txt")
	rename := r.Commit("rename")

	commits, err := gitx.New(r.Dir).CommitsBetween("main", []string{"new.txt", "old.txt"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 || commits[0].Hash != rename || commits[1].Hash != edit {
		t.Fatalf("commits = %+v", commits)
	}
}

func TestCommitsBetweenHonoursLimit(t *testing.T) {
	r := testutil.NewRepo(t)
	r.CommitFile("a.txt", "0\n", "base")
	r.Branch("feature")
	for _, c := range []string{"1\n", "2\n", "3\n"} {
		r.CommitFile("a.txt", c, "change "+c)
	}
	commits, err := gitx.New(r.Dir).CommitsBetween("main", []string{"a.txt"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 {
		t.Fatalf("commits = %d, want 2", len(commits))
	}
}

func TestCommitSubject(t *testing.T) {
	r := testutil.NewRepo(t)
	hash := r.CommitFile("a.txt", "0\n", "the subject line")
	got, err := gitx.New(r.Dir).CommitSubject(hash)
	if err != nil {
		t.Fatal(err)
	}
	if got != "the subject line" {
		t.Fatalf("subject = %q", got)
	}
	if _, err := gitx.New(r.Dir).CommitSubject("--output=x"); err == nil {
		t.Fatal("a non-hash argument must be rejected")
	}
}

func TestLatestCommit(t *testing.T) {
	commits := []gitx.CommitTime{{Hash: "c", Time: 30}, {Hash: "b", Time: 20}, {Hash: "a", Time: 10}}
	got, ok := gitx.LatestCommit(commits, map[string]bool{"c": true})
	if !ok || got.Hash != "b" || got.Time != 20 {
		t.Fatalf("LatestCommit = %+v, %v", got, ok)
	}
	if _, ok := gitx.LatestCommit(commits, map[string]bool{"a": true, "b": true, "c": true}); ok {
		t.Fatal("all ignored must report false")
	}
}
