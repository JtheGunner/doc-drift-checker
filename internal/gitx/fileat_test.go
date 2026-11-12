package gitx_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/gitx"
	"github.com/JtheGunner/doc-drift-checker/internal/testutil"
)

func TestFileAt(t *testing.T) {
	r := testutil.NewRepo(t)
	r.CommitFile("a.go", "package a // v1\n", "v1")
	r.Branch("feature")
	r.CommitFile("a.go", "package a // v2\n", "v2")
	r.CommitFile("dir/*.go", "package star\n", "star")
	c := gitx.New(r.Dir)

	tests := []struct {
		name, rev, path, want string
	}{
		{"head", "HEAD", "a.go", "package a // v2\n"},
		{"older revision", "main", "a.go", "package a // v1\n"},
		{"glob-like file name is literal", "HEAD", "dir/*.go", "package star\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := c.FileAt(tt.rev, tt.path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFileAtMissing(t *testing.T) {
	r := testutil.NewRepo(t)
	r.CommitFile("a.go", "package a\n", "v1")
	c := gitx.New(r.Dir)
	for _, path := range []string{"missing.go", "dir/missing.go", "*.go"} {
		if _, err := c.FileAt("HEAD", path); !errors.Is(err, gitx.ErrNotExist) {
			t.Errorf("FileAt(%q) error = %v, want ErrNotExist", path, err)
		}
	}
}

func TestFileAtBadRevisionIsNotMissing(t *testing.T) {
	r := testutil.NewRepo(t)
	r.CommitFile("a.go", "package a\n", "v1")
	_, err := gitx.New(r.Dir).FileAt("no-such-rev", "a.go")
	if err == nil || errors.Is(err, gitx.ErrNotExist) {
		t.Fatalf("error = %v, want a non-ErrNotExist failure", err)
	}
}

func TestMergeBase(t *testing.T) {
	r := testutil.NewRepo(t)
	fork := r.CommitFile("a.txt", "a\n", "base")
	r.Branch("feature")
	r.CommitFile("b.txt", "b\n", "feature")
	r.Git("checkout", "--quiet", "main")
	r.CommitFile("c.txt", "c\n", "main moves")
	r.Git("checkout", "--quiet", "feature")
	got, err := gitx.New(r.Dir).MergeBase("main")
	if err != nil {
		t.Fatal(err)
	}
	if got != fork {
		t.Fatalf("MergeBase = %q, want %q", got, fork)
	}
}

func TestMergeBaseUnknownRef(t *testing.T) {
	r := testutil.NewRepo(t)
	r.CommitFile("a.txt", "a\n", "base")
	if _, err := gitx.New(r.Dir).MergeBase("nope"); !errors.Is(err, gitx.ErrBaseNotFound) {
		t.Fatalf("error = %v, want ErrBaseNotFound", err)
	}
}

func TestFileSizeAt(t *testing.T) {
	r := testutil.NewRepo(t)
	r.CommitFile("a.go", "12345", "v1")
	c := gitx.New(r.Dir)

	size, err := c.FileSizeAt("HEAD", "a.go")
	if err != nil || size != 5 {
		t.Fatalf("size = %d, err = %v", size, err)
	}
	if _, err := c.FileSizeAt("HEAD", "missing.go"); !errors.Is(err, gitx.ErrNotExist) {
		t.Fatalf("missing file error = %v, want ErrNotExist", err)
	}
}

func TestDirtyFiles(t *testing.T) {
	r := testutil.NewRepo(t)
	r.Write("clean.go", "package clean\n")
	r.Write("worktree.go", "package a\n")
	r.Write("staged.go", "package b\n")
	r.Write("deleted.go", "package c\n")
	r.Write("dir/star*.go", "package d\n")
	r.Commit("base")
	c := gitx.New(r.Dir)

	got, err := c.DirtyFiles()
	if err != nil || len(got) != 0 {
		t.Fatalf("clean tree: %v, %v", got, err)
	}

	r.Write("worktree.go", "package a // edited\n")
	r.Write("staged.go", "package b // edited\n")
	r.Git("add", "staged.go")
	if err := os.Remove(filepath.Join(r.Dir, "deleted.go")); err != nil {
		t.Fatal(err)
	}
	r.Write("dir/star*.go", "package d // edited\n")
	r.Write("untracked.go", "package u\n")

	got, err = c.DirtyFiles()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"deleted.go", "dir/star*.go", "staged.go", "worktree.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DirtyFiles = %v, want %v", got, want)
	}
}
