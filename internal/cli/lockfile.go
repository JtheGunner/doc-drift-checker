package cli

import (
	"errors"
	"fmt"
	"io/fs"

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
