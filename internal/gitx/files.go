package gitx

import (
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
