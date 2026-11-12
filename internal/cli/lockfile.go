package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/JtheGunner/doc-drift-checker/internal/fsx"
	"github.com/JtheGunner/doc-drift-checker/internal/lock"
)

// loadLock reads and validates doc-drift.lock in the repository root. A missing
// lockfile is an empty lock. A symlink, another non-regular file, an oversized
// file or malformed content is an error naming the lockfile.
func loadLock(root string) (lock.Lock, error) {
	data, err := fsx.ReadRegularFileLimit(root, lock.FileName, lock.MaxBytes)
	if errors.Is(err, fs.ErrNotExist) {
		return lock.Lock{}, nil
	}
	if err != nil {
		return lock.Lock{}, fmt.Errorf("read %s: %w", lock.FileName, err)
	}
	l, err := lock.Parse(data)
	if err != nil {
		return lock.Lock{}, fmt.Errorf("%s: %w", lock.FileName, err)
	}
	return l, nil
}

// writeLock atomically replaces doc-drift.lock in the repository root: the data
// goes to a temporary file in the same directory, which is then renamed. It
// refuses to replace a symlink or any other non-regular file.
func writeLock(root string, data []byte) (err error) {
	target := filepath.Join(root, lock.FileName)
	if info, statErr := os.Lstat(target); statErr == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file (symlinks are not followed)", target)
		}
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return fmt.Errorf("stat %s: %w", target, statErr)
	}
	tmp, err := os.CreateTemp(root, ".doc-drift-lock-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary lockfile: %w", err)
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name()) // best effort; the original error matters
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", lock.FileName, err)
	}
	if err = tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", lock.FileName, err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", lock.FileName, err)
	}
	if err = os.Rename(tmp.Name(), target); err != nil {
		return fmt.Errorf("replace %s: %w", lock.FileName, err)
	}
	return nil
}
