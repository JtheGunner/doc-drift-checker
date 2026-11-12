package gitx

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// CommitTime is a commit with its committer time in unix seconds.
type CommitTime struct {
	Hash string
	Time int64
}

// CommitTimes returns the commits touching path, newest first, following renames.
func (c *Client) CommitTimes(path string) ([]CommitTime, error) {
	out, err := c.run("log", "--follow", "--format=%H %ct", "--", path)
	if err != nil {
		return nil, err
	}
	var commits []CommitTime
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		hash, raw, ok := strings.Cut(line, " ")
		if !ok {
			return nil, fmt.Errorf("malformed git log record %q", line)
		}
		t, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("malformed git log record %q", line)
		}
		commits = append(commits, CommitTime{Hash: hash, Time: t})
	}
	return commits, nil
}

// LatestTime returns the newest committer time among the commits that are not
// in ignored. It reports false when no commit remains.
func LatestTime(commits []CommitTime, ignored map[string]bool) (int64, bool) {
	var latest int64
	found := false
	for _, commit := range commits {
		if ignored[commit.Hash] {
			continue
		}
		if !found || commit.Time > latest {
			latest, found = commit.Time, true
		}
	}
	return latest, found
}

// HeadFiles lists the files committed at HEAD, slash-separated and sorted.
// Staged or untracked changes are not included.
func (c *Client) HeadFiles() ([]string, error) {
	root, err := c.Root()
	if err != nil {
		return nil, err
	}
	out, err := c.run("-C", root, "ls-tree", "-r", "--name-only", "-z", "HEAD")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, f := range strings.Split(out, "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	sort.Strings(files)
	return files, nil
}

// ErrShallow is returned when a history-based operation runs in a shallow clone.
var ErrShallow = errors.New("shallow clone")

// EnsureFullHistory fails with ErrShallow when the repository is a shallow
// clone: blame and log would attribute everything to the boundary commit.
func (c *Client) EnsureFullHistory() error {
	if c.isShallow() {
		return fmt.Errorf("%w: history mode needs full history; run git fetch --unshallow", ErrShallow)
	}
	return nil
}

// ParseIgnoreRevs reads a file in git's ignore-revs format: one full commit hash
// per line, blank lines and text after a "#" ignored. Hashes are returned lowercase.
func ParseIgnoreRevs(path string) (map[string]bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("ignore-revs file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("ignore-revs file %s is not a regular file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("ignore-revs file: %w", err)
	}
	revs := map[string]bool{}
	for i, line := range strings.Split(string(data), "\n") {
		line, _, _ = strings.Cut(line, "#")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !isFullHash(line) {
			return nil, fmt.Errorf("ignore-revs file %s: line %d: %q is not a full commit hash", path, i+1, line)
		}
		revs[strings.ToLower(line)] = true
	}
	return revs, nil
}

func isFullHash(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}
