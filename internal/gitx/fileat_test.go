package gitx_test

import (
	"errors"
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
