// Package testutil builds real, deterministic temporary git repositories for tests.
package testutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	authorName  = "Test Author"
	authorEmail = "test@example.com"
)

// baseTime is the timestamp of the first commit; every further commit is one minute later.
var baseTime = time.Date(2024, time.January, 1, 12, 0, 0, 0, time.UTC)

// Repo is a temporary git repository with a fixed identity and deterministic timestamps.
// It has no remote and ignores the user's global and system git configuration.
type Repo struct {
	// Dir is the absolute path of the working tree.
	Dir string

	t       testing.TB
	home    string
	commits int
}

// NewRepo creates an empty repository on branch "main" inside a temp directory.
func NewRepo(t testing.TB) *Repo {
	t.Helper()
	root := t.TempDir()
	r := &Repo{Dir: filepath.Join(root, "repo"), t: t, home: filepath.Join(root, "home")}
	for _, dir := range []string{r.Dir, r.home} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}
	r.Git("init", "--quiet", "--initial-branch=main")
	return r
}

// Env returns the environment that isolates git from the host configuration.
func (r *Repo) Env() []string {
	date := baseTime.Add(time.Duration(r.commits) * time.Minute).Format(time.RFC3339)
	return append(os.Environ(),
		"HOME="+r.home,
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME="+authorName,
		"GIT_AUTHOR_EMAIL="+authorEmail,
		"GIT_COMMITTER_NAME="+authorName,
		"GIT_COMMITTER_EMAIL="+authorEmail,
		"GIT_AUTHOR_DATE="+date,
		"GIT_COMMITTER_DATE="+date,
		"LC_ALL=C",
	)
}

// Git runs git in the repository and returns trimmed stdout. It fails the test on error.
func (r *Repo) Git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	cmd.Env = r.Env()
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// Write creates or overwrites a file (and its parent directories) relative to the repo root.
func (r *Repo) Write(path, content string) {
	r.t.Helper()
	full := filepath.Join(r.Dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		r.t.Fatalf("create dir for %s: %v", path, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		r.t.Fatalf("write %s: %v", path, err)
	}
}

// Move renames a tracked file with git mv.
func (r *Repo) Move(from, to string) {
	r.t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(r.Dir, to)), 0o755); err != nil {
		r.t.Fatalf("create dir for %s: %v", to, err)
	}
	r.Git("mv", from, to)
}

// Remove deletes a tracked file with git rm.
func (r *Repo) Remove(path string) {
	r.t.Helper()
	r.Git("rm", "--quiet", path)
}

// Commit stages everything and commits it with the next deterministic timestamp.
// It returns the full commit SHA.
func (r *Repo) Commit(message string) string {
	r.t.Helper()
	r.Git("add", "--all")
	r.commits++
	r.Git("commit", "--quiet", "--message", message)
	return r.Git("rev-parse", "HEAD")
}

// CommitFile writes one file and commits it.
func (r *Repo) CommitFile(path, content, message string) string {
	r.t.Helper()
	r.Write(path, content)
	return r.Commit(message)
}

// Branch creates and checks out a new branch at HEAD.
func (r *Repo) Branch(name string) {
	r.t.Helper()
	r.Git("checkout", "--quiet", "-b", name)
}

// ShallowClone clones the repository with the given depth into a new temp directory
// (no remote is configured afterwards) and returns it as a Repo.
func (r *Repo) ShallowClone(depth int) *Repo {
	r.t.Helper()
	root := r.t.TempDir()
	clone := &Repo{Dir: filepath.Join(root, "clone"), t: r.t, home: r.home, commits: r.commits}
	r.Git("clone", "--quiet", fmt.Sprintf("--depth=%d", depth), "file://"+r.Dir, clone.Dir)
	clone.Git("remote", "remove", "origin")
	return clone
}
