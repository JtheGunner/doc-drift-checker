package gitx

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Status is the kind of change a file underwent.
type Status string

// File change statuses as reported by git.
const (
	StatusAdded       Status = "A"
	StatusModified    Status = "M"
	StatusDeleted     Status = "D"
	StatusRenamed     Status = "R"
	StatusCopied      Status = "C"
	StatusTypeChanged Status = "T"
)

// Change is one changed file between a base reference and HEAD.
type Change struct {
	Status Status
	// Path is the path at HEAD (the old path for deleted files).
	Path string
	// OldPath is the path at the base; set only for renames and copies.
	OldPath string
}

// Hunk is one contiguous changed region. OldLines is zero for pure additions and
// NewLines for pure deletions; the start is then the line before the change.
type Hunk struct {
	OldStart, OldLines int
	NewStart, NewLines int
}

var errMalformedNameStatus = errors.New("malformed git name-status output")

// ChangedFiles lists files changed between the merge base of base and HEAD (base...HEAD),
// with rename detection, ordered by path.
func (c *Client) ChangedFiles(base string) ([]Change, error) {
	if err := c.ensureBase(base); err != nil {
		return nil, err
	}
	out, err := c.run("diff", "--name-status", "-z", "-M", base+"...HEAD")
	if err != nil {
		return nil, err
	}
	return parseNameStatus(out)
}

func parseNameStatus(out string) ([]Change, error) {
	fields := strings.Split(out, "\x00")
	var changes []Change
	for i := 0; i < len(fields) && fields[i] != ""; {
		status := Status(fields[i][:1])
		pathCount := 1
		if status == StatusRenamed || status == StatusCopied {
			pathCount = 2
		}
		if i+pathCount >= len(fields) {
			return nil, errMalformedNameStatus
		}
		change := Change{Status: status, Path: fields[i+pathCount]}
		if pathCount == 2 {
			change.OldPath = fields[i+1]
		}
		changes = append(changes, change)
		i += 1 + pathCount
	}
	return changes, nil
}

// Hunks returns the changed regions of one file between the merge base of base and HEAD.
// For renames the old path is diffed too, so a pure rename yields no hunks.
func (c *Client) Hunks(base string, change Change) ([]Hunk, error) {
	if err := c.ensureBase(base); err != nil {
		return nil, err
	}
	args := []string{"diff", "-U0", "-M", base + "...HEAD", "--", change.Path}
	if change.OldPath != "" {
		args = append(args, change.OldPath)
	}
	out, err := c.run(args...)
	if err != nil {
		return nil, err
	}
	var hunks []Hunk
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "@@ ") {
			continue
		}
		h, err := parseHunkHeader(line)
		if err != nil {
			return nil, err
		}
		hunks = append(hunks, h)
	}
	return hunks, nil
}

// parseHunkHeader parses "@@ -a[,b] +c[,d] @@ ...".
func parseHunkHeader(line string) (Hunk, error) {
	parts := strings.Fields(line)
	if len(parts) < 3 || !strings.HasPrefix(parts[1], "-") || !strings.HasPrefix(parts[2], "+") {
		return Hunk{}, fmt.Errorf("malformed hunk header %q", line)
	}
	oldStart, oldLines, err := parseRange(parts[1][1:])
	if err != nil {
		return Hunk{}, fmt.Errorf("hunk header %q: %w", line, err)
	}
	newStart, newLines, err := parseRange(parts[2][1:])
	if err != nil {
		return Hunk{}, fmt.Errorf("hunk header %q: %w", line, err)
	}
	return Hunk{OldStart: oldStart, OldLines: oldLines, NewStart: newStart, NewLines: newLines}, nil
}

// parseRange parses "start" or "start,count"; a missing count means 1.
func parseRange(s string) (start, count int, err error) {
	startText, countText, hasCount := strings.Cut(s, ",")
	if start, err = strconv.Atoi(startText); err != nil {
		return 0, 0, err
	}
	if !hasCount {
		return start, 1, nil
	}
	count, err = strconv.Atoi(countText)
	return start, count, err
}
