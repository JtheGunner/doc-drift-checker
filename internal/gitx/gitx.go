// Package gitx is a thin wrapper around the git CLI. It exposes the data needed for
// drift detection: changed files, diff hunks, blame and log.
package gitx

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ErrBaseNotFound is returned when the base reference cannot be resolved or shares no
// history with HEAD. In shallow clones this usually means the history is truncated.
var ErrBaseNotFound = errors.New("base reference not found")

// Error is a failed git invocation. Its message includes git's stderr output.
type Error struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *Error) Error() string {
	msg := fmt.Sprintf("git %s: %v", strings.Join(e.Args, " "), e.Err)
	if e.Stderr != "" {
		msg += ": " + e.Stderr
	}
	return msg
}

func (e *Error) Unwrap() error { return e.Err }

// Client runs git commands inside a working directory.
type Client struct {
	dir string
}

// New returns a Client operating on the repository containing dir.
func New(dir string) *Client {
	return &Client{dir: dir}
}

func (c *Client) run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = c.dir
	cmd.Env = append(os.Environ(), "LC_ALL=C", "GIT_TERMINAL_PROMPT=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), &Error{Args: args, Stderr: strings.TrimSpace(stderr.String()), Err: err}
	}
	return stdout.String(), nil
}

func (c *Client) isShallow() bool {
	out, err := c.run("rev-parse", "--is-shallow-repository")
	return err == nil && strings.TrimSpace(out) == "true"
}

// ensureBase verifies that base resolves to a commit and has a merge base with HEAD.
func (c *Client) ensureBase(base string) error {
	hint := ""
	if c.isShallow() {
		hint = " (this is a shallow clone: fetch more history, e.g. with git fetch --unshallow or a larger --depth)"
	}
	if _, err := c.run("rev-parse", "--verify", "--quiet", base+"^{commit}"); err != nil {
		return fmt.Errorf("%w: %q does not resolve to a commit%s", ErrBaseNotFound, base, hint)
	}
	if _, err := c.run("merge-base", base, "HEAD"); err != nil {
		var gerr *Error
		if errors.As(err, &gerr) && gerr.Stderr == "" {
			return fmt.Errorf("%w: %q shares no history with HEAD%s", ErrBaseNotFound, base, hint)
		}
		return err
	}
	return nil
}
