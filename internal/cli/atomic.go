package cli

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
)

// writeFileAtomic replaces the regular file rel (slash-separated, relative to
// root) with data: the data goes to a temporary file in the same directory,
// which is then renamed over the target. All access goes through os.OpenRoot,
// so no symlink in a directory component can lead outside root. A symlink or
// any other non-regular file at rel is refused, never followed. An existing
// file keeps its permission bits; a new file gets perm.
func writeFileAtomic(root, rel string, data []byte, perm fs.FileMode) (err error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return fmt.Errorf("open %s: %w", root, err)
	}
	defer func() { _ = r.Close() }() // the data is already renamed or the write already failed

	if info, statErr := r.Lstat(rel); statErr == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file (symlinks are not followed)", rel)
		}
		perm = info.Mode().Perm()
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return fmt.Errorf("stat %s: %w", rel, statErr)
	}

	suffix := make([]byte, 8)
	if _, err = rand.Read(suffix); err != nil {
		return fmt.Errorf("create temporary file for %s: %w", rel, err)
	}
	tmpName := path.Join(path.Dir(rel), ".doc-drift-"+hex.EncodeToString(suffix)+".tmp")
	tmp, err := r.OpenFile(tmpName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create temporary file for %s: %w", rel, err)
	}
	defer func() {
		if err != nil {
			_ = r.Remove(tmpName) // best effort; the original error matters
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", rel, err)
	}
	if err = tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", rel, err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", rel, err)
	}
	if err = r.Rename(tmpName, rel); err != nil {
		return fmt.Errorf("replace %s: %w", rel, err)
	}
	return nil
}
