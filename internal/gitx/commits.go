package gitx

import (
	"fmt"
	"strconv"
	"strings"
)

// CommitInfo is one commit with its committer time in unix seconds. The subject
// is untrusted text from the repository.
type CommitInfo struct {
	Hash    string
	Time    int64
	Subject string
}

// CommitsBetween lists the commits that touch any of paths between the merge
// base of base and HEAD (base..HEAD), newest first. A limit above zero caps the
// number of commits returned. Pass both paths of a rename to follow it.
func (c *Client) CommitsBetween(base string, paths []string, limit int) ([]CommitInfo, error) {
	args := []string{"log", "-z", "--format=%H%x1f%ct%x1f%s"}
	if limit > 0 {
		args = append(args, "-n", strconv.Itoa(limit))
	}
	args = append(args, base+"..HEAD", "--")
	out, err := c.run(append(args, paths...)...)
	if err != nil {
		return nil, err
	}
	var commits []CommitInfo
	for _, rec := range strings.Split(out, "\x00") {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		f := strings.SplitN(rec, "\x1f", 3)
		if len(f) != 3 {
			return nil, fmt.Errorf("malformed git log record %q", rec)
		}
		t, err := strconv.ParseInt(f[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("malformed git log record %q", rec)
		}
		commits = append(commits, CommitInfo{Hash: f[0], Time: t, Subject: f[2]})
	}
	return commits, nil
}

// CommitSubject returns the subject line of the commit with the given full hash.
func (c *Client) CommitSubject(hash string) (string, error) {
	if !isFullHash(hash) {
		return "", fmt.Errorf("%q is not a full commit hash", hash)
	}
	out, err := c.run("show", "-s", "--format=%s", hash, "--")
	if err != nil {
		return "", err
	}
	return strings.TrimRight(out, "\n"), nil
}
