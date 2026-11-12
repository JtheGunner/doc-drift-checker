package fsx

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
}

func wantRejected(t *testing.T, err error, rel string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), rel) || !strings.Contains(err.Error(), "regular file") {
		t.Errorf("error %q should name %q and mention a regular file", err, rel)
	}
}

func TestReadRegularFile(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	write(t, filepath.Join(root, "ok.md"), "hello")
	write(t, filepath.Join(root, "real", "inner.md"), "inner")
	write(t, filepath.Join(outside, "secret.md"), "secret")
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Run("regular file", func(t *testing.T) {
		got, err := ReadRegularFile(root, "ok.md")
		if err != nil || string(got) != "hello" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("regular file in subdirectory", func(t *testing.T) {
		got, err := ReadRegularFile(root, "real/inner.md")
		if err != nil || string(got) != "inner" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("missing file", func(t *testing.T) {
		_, err := ReadRegularFile(root, "missing.md")
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("error %v should wrap fs.ErrNotExist", err)
		}
	})
	t.Run("missing root", func(t *testing.T) {
		_, err := ReadRegularFile(filepath.Join(root, "nope"), "ok.md")
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("error %v should wrap fs.ErrNotExist", err)
		}
	})
	t.Run("symlink to file inside root", func(t *testing.T) {
		symlink(t, filepath.Join(root, "ok.md"), filepath.Join(root, "link-in.md"))
		_, err := ReadRegularFile(root, "link-in.md")
		wantRejected(t, err, "link-in.md")
	})
	t.Run("symlink to file outside root", func(t *testing.T) {
		symlink(t, filepath.Join(outside, "secret.md"), filepath.Join(root, "link-out.md"))
		_, err := ReadRegularFile(root, "link-out.md")
		wantRejected(t, err, "link-out.md")
	})
	t.Run("symlinked intermediate directory outside root", func(t *testing.T) {
		symlink(t, outside, filepath.Join(root, "docs"))
		_, err := ReadRegularFile(root, "docs/secret.md")
		wantRejected(t, err, "docs/secret.md")
	})
	t.Run("directory", func(t *testing.T) {
		_, err := ReadRegularFile(root, "dir")
		wantRejected(t, err, "dir")
	})
}
