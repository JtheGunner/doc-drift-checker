package gitx

import (
	"slices"
	"sort"
	"strings"
)

// TrackedFiles lists the files tracked in the index of the repository, as
// slash-separated paths relative to the repository root, sorted.
// It runs from the Client directory but always lists the whole repository.
func (c *Client) TrackedFiles() ([]string, error) {
	// Listing from the root avoids the ":/" magic pathspec, which
	// GIT_LITERAL_PATHSPECS would turn into a file name.
	root, err := c.Root()
	if err != nil {
		return nil, err
	}
	out, err := c.run("-C", root, "ls-files", "-z")
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

// Root returns the absolute path of the repository's top-level directory.
func (c *Client) Root() (string, error) {
	out, err := c.run("rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// DirtyFiles lists the tracked files whose working tree or index content
// differs from HEAD (modified, staged, deleted, type-changed), as
// slash-separated paths relative to the repository root, sorted. Untracked
// files are not listed.
func (c *Client) DirtyFiles() ([]string, error) {
	root, err := c.Root()
	if err != nil {
		return nil, err
	}
	// Two diffs: HEAD against the index, and the index against the working
	// tree. A file edited and reverted in the working tree after staging is
	// still reported through the first one.
	var files []string
	for _, args := range [][]string{
		{"-C", root, "diff", "--name-only", "-z", "--no-renames", "--cached", "HEAD"},
		{"-C", root, "diff", "--name-only", "-z", "--no-renames"},
	} {
		out, err := c.run(args...)
		if err != nil {
			return nil, err
		}
		for _, f := range strings.Split(out, "\x00") {
			if f != "" {
				files = append(files, f)
			}
		}
	}
	sort.Strings(files)
	return slices.Compact(files), nil
}
