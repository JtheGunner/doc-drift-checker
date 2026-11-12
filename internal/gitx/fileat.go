package gitx

import (
	"errors"
	"strings"
)

// ErrNotExist is returned by FileAt when the file is absent at the revision.
var ErrNotExist = errors.New("file does not exist at revision")

// FileAt returns the content of path (relative to the repository root) at the
// given revision. A file that is absent at that revision yields ErrNotExist.
// The rev:path form is an object name, not a pathspec, so file names with glob
// characters are read literally.
func (c *Client) FileAt(rev, path string) ([]byte, error) {
	out, err := c.run("cat-file", "blob", rev+":"+path)
	if err == nil {
		return []byte(out), nil
	}
	var gerr *Error
	if errors.As(err, &gerr) && isMissingPath(gerr.Stderr) {
		return nil, ErrNotExist
	}
	return nil, err
}

// isMissingPath recognises git's messages for a path that is not in a tree.
func isMissingPath(stderr string) bool {
	return strings.Contains(stderr, "does not exist in") ||
		strings.Contains(stderr, "exists on disk, but not in")
}

// MergeBase returns the full SHA of the merge base of base and HEAD, the
// revision that base...HEAD diffs start from.
func (c *Client) MergeBase(base string) (string, error) {
	if err := c.ensureBase(base); err != nil {
		return "", err
	}
	out, err := c.run("merge-base", base, "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
