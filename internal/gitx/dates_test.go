package gitx_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/gitx"
	"github.com/JtheGunner/doc-drift-checker/internal/testutil"
)

// commitTime is the committer time of the n-th commit made through testutil.
func commitTime(n int) int64 {
	return 1704110400 + int64(n)*60
}

func writeIgnoreRevs(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ignore-revs")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBlameIgnoringSkipsFormattingCommit(t *testing.T) {
	r := testutil.NewRepo(t)
	first := r.CommitFile("f.txt", "alpha\nbeta\n", "first")
	format := r.CommitFile("f.txt", "alpha \nbeta \n", "format")
	g := gitx.New(r.Dir)

	plain, err := g.Blame("f.txt", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if plain[0].Commit != format || plain[0].Time != commitTime(2) {
		t.Fatalf("plain blame = %+v, want the format commit", plain[0])
	}

	ignored, err := g.BlameIgnoring("f.txt", 1, 2, writeIgnoreRevs(t, format+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range ignored {
		if l.Commit != first || l.Time != commitTime(1) {
			t.Fatalf("ignored blame = %+v, want the first commit", l)
		}
	}
}

func TestCommitTimes(t *testing.T) {
	r := testutil.NewRepo(t)
	first := r.CommitFile("f.txt", "a\n", "first")
	r.CommitFile("other.txt", "o\n", "unrelated")
	third := r.CommitFile("f.txt", "b\n", "third")
	got, err := gitx.New(r.Dir).CommitTimes("f.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := []gitx.CommitTime{{Hash: third, Time: commitTime(3)}, {Hash: first, Time: commitTime(1)}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestCommitTimesFollowsRenames(t *testing.T) {
	r := testutil.NewRepo(t)
	r.CommitFile("old.txt", "some content\nmore content\n", "create")
	r.Move("old.txt", "new.txt")
	r.Commit("rename")
	got, err := gitx.New(r.Dir).CommitTimes("new.txt")
	if err != nil || len(got) != 2 {
		t.Fatalf("got %+v, %v; want history across the rename", got, err)
	}
}

func TestLatestTime(t *testing.T) {
	commits := []gitx.CommitTime{{Hash: "c", Time: 30}, {Hash: "b", Time: 20}, {Hash: "a", Time: 10}}
	tests := []struct {
		name    string
		ignored map[string]bool
		want    int64
		wantOK  bool
	}{
		{name: "newest", want: 30, wantOK: true},
		{name: "newest ignored", ignored: map[string]bool{"c": true}, want: 20, wantOK: true},
		{name: "all ignored", ignored: map[string]bool{"a": true, "b": true, "c": true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := gitx.LatestTime(commits, tt.ignored)
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("got %d, %v; want %d, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
	if _, ok := gitx.LatestTime(nil, nil); ok {
		t.Fatal("no commits must report no time")
	}
}

func TestParseIgnoreRevs(t *testing.T) {
	a := strings.Repeat("a", 40)
	b := strings.Repeat("B", 40)
	path := writeIgnoreRevs(t, "# formatting\n\n"+a+"\n  "+b+"  # inline comment\n")
	got, err := gitx.ParseIgnoreRevs(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{a: true, strings.ToLower(b): true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestParseIgnoreRevsErrors(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name    string
		path    string
		wantMsg string
	}{
		{name: "missing", path: filepath.Join(dir, "nope"), wantMsg: "nope"},
		{name: "directory", path: dir, wantMsg: "regular file"},
		{name: "abbreviated hash", path: writeIgnoreRevs(t, "# c\nabc123\n"), wantMsg: "line 2"},
		{name: "not hex", path: writeIgnoreRevs(t, strings.Repeat("z", 40)+"\n"), wantMsg: "line 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := gitx.ParseIgnoreRevs(tt.path)
			if err == nil || !strings.Contains(err.Error(), tt.wantMsg) {
				t.Fatalf("got %v, want an error containing %q", err, tt.wantMsg)
			}
		})
	}
}

func TestHeadFiles(t *testing.T) {
	r := testutil.NewRepo(t)
	r.CommitFile("a.txt", "a\n", "first")
	r.Write("staged.txt", "s\n")
	r.Git("add", "staged.txt")
	got, err := gitx.New(r.Dir).HeadFiles()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"a.txt"}) {
		t.Fatalf("got %v, want only the committed file", got)
	}
}

func TestEnsureFullHistory(t *testing.T) {
	r := testutil.NewRepo(t)
	r.CommitFile("a.txt", "1\n", "one")
	r.CommitFile("a.txt", "2\n", "two")
	if err := gitx.New(r.Dir).EnsureFullHistory(); err != nil {
		t.Fatalf("full repository: %v", err)
	}
	err := gitx.New(r.ShallowClone(1).Dir).EnsureFullHistory()
	if err == nil || !strings.Contains(err.Error(), "git fetch --unshallow") {
		t.Fatalf("shallow clone: got %v, want a hint to unshallow", err)
	}
}
