package gitx

import (
	"fmt"
	"strconv"
	"strings"
)

// BlameLine attributes one line of a file at HEAD to a commit.
type BlameLine struct {
	Line    int
	Commit  string
	Author  string
	Summary string
}

// Commit is one entry of a file's history.
type Commit struct {
	Hash    string
	Author  string
	Date    string // strict ISO 8601
	Subject string
}

// Blame returns the blame of lines start..end (1-based, inclusive) of path at HEAD.
func (c *Client) Blame(path string, start, end int) ([]BlameLine, error) {
	if start < 1 || end < start {
		return nil, fmt.Errorf("invalid blame range %d-%d", start, end)
	}
	out, err := c.run("blame", "--porcelain", "-L", fmt.Sprintf("%d,%d", start, end), "HEAD", "--", path)
	if err != nil {
		return nil, err
	}
	return parseBlame(out)
}

// parseBlame parses porcelain output. Per line git emits a header
// "<sha> <orig-line> <final-line> [<group-size>]", optional metadata (only on the first
// occurrence of a commit) and the line content prefixed with a tab.
func parseBlame(out string) ([]BlameLine, error) {
	type meta struct{ author, summary string }
	metas := map[string]*meta{}
	var lines []BlameLine
	var current *meta
	var commit string
	var finalLine int
	expectHeader := true
	for _, raw := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(raw, "\t"):
			lines = append(lines, BlameLine{Line: finalLine, Commit: commit, Author: current.author, Summary: current.summary})
			expectHeader = true
		case expectHeader:
			f := strings.Fields(raw)
			if len(f) < 3 {
				continue
			}
			n, err := strconv.Atoi(f[2])
			if err != nil {
				return nil, fmt.Errorf("malformed blame header %q", raw)
			}
			commit, finalLine = f[0], n
			if metas[commit] == nil {
				metas[commit] = &meta{}
			}
			current = metas[commit]
			expectHeader = false
		case strings.HasPrefix(raw, "author "):
			current.author = strings.TrimPrefix(raw, "author ")
		case strings.HasPrefix(raw, "summary "):
			current.summary = strings.TrimPrefix(raw, "summary ")
		}
	}
	return lines, nil
}

// Log returns the commits touching path, newest first, following renames.
func (c *Client) Log(path string) ([]Commit, error) {
	const fieldSep, recordSep = "\x1f", "\x1e"
	format := "--format=" + strings.Join([]string{"%H", "%an", "%aI", "%s"}, fieldSep) + recordSep
	out, err := c.run("log", "--follow", format, "--", path)
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for _, rec := range strings.Split(out, recordSep) {
		rec = strings.TrimSpace(rec)
		if rec == "" {
			continue
		}
		f := strings.Split(rec, fieldSep)
		if len(f) != 4 {
			return nil, fmt.Errorf("malformed git log record %q", rec)
		}
		commits = append(commits, Commit{Hash: f[0], Author: f[1], Date: f[2], Subject: f[3]})
	}
	return commits, nil
}
