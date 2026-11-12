// Package fsx holds small filesystem helpers shared by the commands.
package fsx

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// ReadRegularFile reads the file rel (a path relative to root) only if it is a
// regular file inside root. Path resolution goes through os.OpenRoot, so no
// symlink in any component can lead outside root. A symlink as the final
// component, and any other non-regular file (directory, device, FIFO), is
// rejected with an error naming the path. The check and the read use the same
// open handle, so the file cannot be swapped in between. A missing file
// returns an error wrapping fs.ErrNotExist.
func ReadRegularFile(root, rel string) ([]byte, error) {
	full := filepath.Join(root, rel)
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }() // read-only handle

	link, err := r.Lstat(rel)
	if err != nil {
		return nil, rejectOrPassThrough(full, err)
	}
	if !link.Mode().IsRegular() {
		return nil, notRegular(full)
	}
	f, err := r.Open(rel)
	if err != nil {
		return nil, rejectOrPassThrough(full, err)
	}
	defer func() { _ = f.Close() }() // read-only handle
	opened, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", full, err)
	}
	if !opened.Mode().IsRegular() || !os.SameFile(link, opened) {
		return nil, notRegular(full)
	}
	return io.ReadAll(f)
}

// rejectOrPassThrough keeps not-exist errors intact and reports every other
// failure (including a path escaping the root) as a rejected file.
func rejectOrPassThrough(full string, err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return fmt.Errorf("%s is not a regular file inside the repository: %w", full, err)
}

func notRegular(full string) error {
	return fmt.Errorf("%s is not a regular file (symlinks are not followed)", full)
}
