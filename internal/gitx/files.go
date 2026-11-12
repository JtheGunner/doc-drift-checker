package gitx

import (
	"sort"
	"strings"
)

// TrackedFiles lists the files tracked in the index of the repository, as
// slash-separated paths relative to the repository root, sorted.
// It runs from the Client directory but always lists the whole repository.
func (c *Client) TrackedFiles() ([]string, error) {
	out, err := c.run("ls-files", "-z", "--full-name", ":/")
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
