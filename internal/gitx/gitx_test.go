package gitx_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/gitx"
	"github.com/JtheGunner/doc-drift-checker/internal/testutil"
)

const lines = "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\n"

// featureRepo has base commit on main and a feature branch with a modify, add, delete and rename.
func featureRepo(t *testing.T) *testutil.Repo {
	t.Helper()
	r := testutil.NewRepo(t)
	r.Write("mod.txt", lines)
	r.Write("del.txt", "to be deleted\n")
	r.Write("old/name.txt", lines+"unique rename content\n")
	r.Commit("base")
	r.Branch("feature")
	r.Write("mod.txt", strings.Replace(lines, "line2", "LINE2", 1))
	r.Write("add.txt", "new\n")
	r.Remove("del.txt")
	r.Move("old/name.txt", "new/name.txt")
	r.Commit("feature work")
	return r
}

func TestChangedFiles(t *testing.T) {
	r := featureRepo(t)
	got, err := gitx.New(r.Dir).ChangedFiles("main")
	if err != nil {
		t.Fatal(err)
	}
	want := []gitx.Change{
		{Status: gitx.StatusAdded, Path: "add.txt"},
		{Status: gitx.StatusDeleted, Path: "del.txt"},
		{Status: gitx.StatusModified, Path: "mod.txt"},
		{Status: gitx.StatusRenamed, Path: "new/name.txt", OldPath: "old/name.txt"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestChangedFilesRenameIsNotDeleteAndAdd(t *testing.T) {
	r := featureRepo(t)
	got, _ := gitx.New(r.Dir).ChangedFiles("main")
	for _, c := range got {
		if c.Path == "old/name.txt" || (c.Path == "new/name.txt" && c.Status == gitx.StatusAdded) {
			t.Fatalf("rename reported as delete plus add: %+v", c)
		}
	}
}

func TestChangedFilesUsesMergeBase(t *testing.T) {
	r := featureRepo(t)
	r.Git("checkout", "--quiet", "main")
	r.CommitFile("only-on-main.txt", "x\n", "main moves on")
	r.Git("checkout", "--quiet", "feature")
	got, err := gitx.New(r.Dir).ChangedFiles("main")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range got {
		if c.Path == "only-on-main.txt" {
			t.Fatalf("change from base branch leaked into three-dot diff: %+v", c)
		}
	}
}

func TestChangedFilesNoChanges(t *testing.T) {
	r := testutil.NewRepo(t)
	r.CommitFile("a.txt", "a\n", "base")
	got, err := gitx.New(r.Dir).ChangedFiles("main")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no changes, got %+v", got)
	}
}

func TestChangedFilesUnknownBase(t *testing.T) {
	r := featureRepo(t)
	_, err := gitx.New(r.Dir).ChangedFiles("does-not-exist")
	if !errors.Is(err, gitx.ErrBaseNotFound) {
		t.Fatalf("expected ErrBaseNotFound, got %v", err)
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Fatalf("message should name the ref: %v", err)
	}
}

func TestChangedFilesShallowCloneMissingBase(t *testing.T) {
	r := featureRepo(t)
	clone := r.ShallowClone(1)
	// The clone has only the feature tip; "main" does not exist locally.
	_, err := gitx.New(clone.Dir).ChangedFiles("main")
	if !errors.Is(err, gitx.ErrBaseNotFound) {
		t.Fatalf("expected ErrBaseNotFound, got %v", err)
	}
	if !strings.Contains(err.Error(), "shallow") {
		t.Fatalf("message should mention shallow clone: %v", err)
	}
}

func TestChangedFilesShallowCloneNoMergeBase(t *testing.T) {
	r := testutil.NewRepo(t)
	r.CommitFile("a.txt", "1\n", "one")
	base := r.Git("rev-parse", "HEAD")
	r.CommitFile("a.txt", "2\n", "two")
	r.CommitFile("a.txt", "3\n", "three")
	clone := r.ShallowClone(1)
	// Make the old commit available as a ref without its history.
	clone.Git("fetch", "--quiet", "--depth=1", "file://"+r.Dir, base+":refs/heads/base")
	_, err := gitx.New(clone.Dir).ChangedFiles("base")
	if !errors.Is(err, gitx.ErrBaseNotFound) {
		t.Fatalf("expected ErrBaseNotFound, got %v", err)
	}
	if !strings.Contains(err.Error(), "shallow") {
		t.Fatalf("message should mention shallow clone: %v", err)
	}
}

func TestErrorIncludesStderr(t *testing.T) {
	_, err := gitx.New(t.TempDir()).Log("a.txt")
	if err == nil {
		t.Fatal("expected error outside a repository")
	}
	var gerr *gitx.Error
	if !errors.As(err, &gerr) {
		t.Fatalf("expected *gitx.Error, got %T", err)
	}
	if !strings.Contains(gerr.Stderr, "not a git repository") || !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("stderr missing from error: %q / %v", gerr.Stderr, err)
	}
}

func TestHunks(t *testing.T) {
	r := featureRepo(t)
	g := gitx.New(r.Dir)
	got, err := g.Hunks("main", gitx.Change{Status: gitx.StatusModified, Path: "mod.txt"})
	if err != nil {
		t.Fatal(err)
	}
	want := []gitx.Hunk{{OldStart: 2, OldLines: 1, NewStart: 2, NewLines: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestHunksAddedAndDeleted(t *testing.T) {
	r := featureRepo(t)
	g := gitx.New(r.Dir)
	added, err := g.Hunks("main", gitx.Change{Status: gitx.StatusAdded, Path: "add.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []gitx.Hunk{{OldStart: 0, OldLines: 0, NewStart: 1, NewLines: 1}}; !reflect.DeepEqual(added, want) {
		t.Fatalf("added: got %+v want %+v", added, want)
	}
	deleted, err := g.Hunks("main", gitx.Change{Status: gitx.StatusDeleted, Path: "del.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []gitx.Hunk{{OldStart: 1, OldLines: 1, NewStart: 0, NewLines: 0}}; !reflect.DeepEqual(deleted, want) {
		t.Fatalf("deleted: got %+v want %+v", deleted, want)
	}
}

func TestHunksPureRenameHasNone(t *testing.T) {
	r := featureRepo(t)
	got, err := gitx.New(r.Dir).Hunks("main", gitx.Change{Status: gitx.StatusRenamed, Path: "new/name.txt", OldPath: "old/name.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("pure rename should have no hunks, got %+v", got)
	}
}

func TestHunksMultiple(t *testing.T) {
	r := testutil.NewRepo(t)
	var original strings.Builder
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&original, "row%d\n", i)
	}
	r.CommitFile("f.txt", original.String(), "base")
	r.Branch("feature")
	content := strings.Split(original.String(), "\n")
	content[1] = "a"
	content[15] = "b"
	r.CommitFile("f.txt", strings.Join(content, "\n"), "edit")
	got, err := gitx.New(r.Dir).Hunks("main", gitx.Change{Status: gitx.StatusModified, Path: "f.txt"})
	if err != nil {
		t.Fatal(err)
	}
	want := []gitx.Hunk{
		{OldStart: 2, OldLines: 1, NewStart: 2, NewLines: 1},
		{OldStart: 16, OldLines: 1, NewStart: 16, NewLines: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestBlame(t *testing.T) {
	r := testutil.NewRepo(t)
	first := r.CommitFile("f.txt", "a\nb\nc\n", "first")
	second := r.CommitFile("f.txt", "a\nB\nc\n", "second")
	got, err := gitx.New(r.Dir).Blame("f.txt", 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []gitx.BlameLine{
		{Line: 1, Commit: first, Author: "Test Author", Summary: "first"},
		{Line: 2, Commit: second, Author: "Test Author", Summary: "second"},
		{Line: 3, Commit: first, Author: "Test Author", Summary: "first"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestBlameSubRange(t *testing.T) {
	r := testutil.NewRepo(t)
	r.CommitFile("f.txt", "a\nb\nc\nd\n", "first")
	got, err := gitx.New(r.Dir).Blame("f.txt", 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Line != 2 || got[1].Line != 3 {
		t.Fatalf("unexpected range: %+v", got)
	}
}

func TestBlameErrors(t *testing.T) {
	r := testutil.NewRepo(t)
	r.CommitFile("f.txt", "a\n", "first")
	g := gitx.New(r.Dir)
	if _, err := g.Blame("f.txt", 0, 1); err == nil {
		t.Fatal("expected error for invalid range")
	}
	if _, err := g.Blame("f.txt", 3, 2); err == nil {
		t.Fatal("expected error for reversed range")
	}
	_, err := g.Blame("missing.txt", 1, 1)
	if err == nil || !strings.Contains(err.Error(), "missing.txt") {
		t.Fatalf("expected stderr naming the file, got %v", err)
	}
}

func TestLog(t *testing.T) {
	r := testutil.NewRepo(t)
	first := r.CommitFile("f.txt", "a\n", "first")
	r.CommitFile("other.txt", "o\n", "unrelated")
	second := r.CommitFile("f.txt", "b\n", "second: with colon")
	got, err := gitx.New(r.Dir).Log("f.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := []gitx.Commit{
		{Hash: second, Author: "Test Author", Date: "2024-01-01T12:03:00Z", Subject: "second: with colon"},
		{Hash: first, Author: "Test Author", Date: "2024-01-01T12:01:00Z", Subject: "first"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestLogFollowsRenames(t *testing.T) {
	r := testutil.NewRepo(t)
	r.CommitFile("old.txt", "some content\nmore content\n", "create")
	r.Move("old.txt", "new.txt")
	r.Commit("rename")
	got, err := gitx.New(r.Dir).Log("new.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected history across rename, got %+v", got)
	}
}

func TestLogUnknownPathIsEmpty(t *testing.T) {
	r := testutil.NewRepo(t)
	r.CommitFile("f.txt", "a\n", "first")
	got, err := gitx.New(r.Dir).Log("nope.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty log, got %+v", got)
	}
}
