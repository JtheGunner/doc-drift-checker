package relevance

import (
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/config"
	"github.com/JtheGunner/doc-drift-checker/internal/gitx"
)

func TestDefaultIgnored(t *testing.T) {
	f := NewFilter(config.DefaultIgnore())
	tests := []struct {
		path string
		want bool
	}{
		// tests
		{"foo_test.go", true},
		{"internal/cli/check_test.go", true},
		{"web/app.test.ts", true},
		{"web/app.spec.js", true},
		{"test/helper.go", true},
		{"pkg/tests/a.py", true},
		{"src/__tests__/a.js", true},
		{"internal/x/testdata/in.txt", true},
		// generated
		{"api/service.pb.go", true},
		{"internal/models_gen.go", true},
		{"src/schema.generated.ts", true},
		{"src/generated/types.go", true},
		{"static/app.min.js", true},
		// lockfiles, at root and nested
		{"go.sum", true},
		{"web/package-lock.json", true},
		{"yarn.lock", true},
		{"pnpm-lock.yaml", true},
		{"composer.lock", true},
		{"Cargo.lock", true},
		{"Gemfile.lock", true},
		{"poetry.lock", true},
		{"uv.lock", true},
		// vendored
		{"vendor/github.com/x/y.go", true},
		{"web/node_modules/a/index.js", true},
		{"third_party/lib/a.c", true},
		// relevant files that must not be ignored
		{"main.go", false},
		{"internal/cli/check.go", false},
		{"README.md", false},
		{"go.mod", false},
		{"src/latest.go", false},
		{"src/contest.go", false},
		{"src/testing.go", false},
		{"cmd/vendorish/main.go", false},
		{"docs/go.sum.md", false},
		{"src/test.go", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := f.Ignored(tt.path); got != tt.want {
				t.Errorf("Ignored(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestEmptyFilterIgnoresNothing(t *testing.T) {
	if NewFilter(nil).Ignored("foo_test.go") {
		t.Error("a filter without globs must not ignore anything")
	}
}

func TestPathIgnored(t *testing.T) {
	f := NewFilter(config.DefaultIgnore())
	tests := []struct {
		name   string
		change gitx.Change
		want   bool
	}{
		{"modified ignored", gitx.Change{Status: gitx.StatusModified, Path: "a_test.go"}, true},
		{"modified relevant", gitx.Change{Status: gitx.StatusModified, Path: "a.go"}, false},
		{"added ignored", gitx.Change{Status: gitx.StatusAdded, Path: "go.sum"}, true},
		{"deleted ignored", gitx.Change{Status: gitx.StatusDeleted, Path: "vendor/x.go"}, true},
		{"rename within ignored", gitx.Change{Status: gitx.StatusRenamed, OldPath: "a_test.go", Path: "b_test.go"}, true},
		{"rename into ignored", gitx.Change{Status: gitx.StatusRenamed, OldPath: "a.go", Path: "a_test.go"}, false},
		{"rename out of ignored", gitx.Change{Status: gitx.StatusRenamed, OldPath: "a_test.go", Path: "a.go"}, false},
		{"rename between relevant", gitx.Change{Status: gitx.StatusRenamed, OldPath: "a.go", Path: "b.go"}, false},
		{"copy from relevant to ignored", gitx.Change{Status: gitx.StatusCopied, OldPath: "a.go", Path: "a_test.go"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := f.PathIgnored(tt.change); got != tt.want {
				t.Errorf("PathIgnored(%+v) = %v, want %v", tt.change, got, tt.want)
			}
		})
	}
}

func TestRelevant(t *testing.T) {
	f := NewFilter(config.DefaultIgnore())
	tests := []struct {
		name           string
		change         gitx.Change
		whitespaceOnly bool
		want           bool
	}{
		{"relevant modification", gitx.Change{Status: gitx.StatusModified, Path: "a.go"}, false, true},
		{"whitespace-only modification", gitx.Change{Status: gitx.StatusModified, Path: "a.go"}, true, false},
		{"ignored modification", gitx.Change{Status: gitx.StatusModified, Path: "a_test.go"}, false, false},
		{"whitespace flag never drops additions", gitx.Change{Status: gitx.StatusAdded, Path: "a.go"}, true, true},
		{"whitespace flag never drops deletions", gitx.Change{Status: gitx.StatusDeleted, Path: "a.go"}, true, true},
		{"whitespace flag never drops renames", gitx.Change{Status: gitx.StatusRenamed, OldPath: "a.go", Path: "b.go"}, true, true},
		{"whitespace flag never drops copies", gitx.Change{Status: gitx.StatusCopied, OldPath: "a.go", Path: "b.go"}, true, true},
		{"ignored rename stays dropped", gitx.Change{Status: gitx.StatusRenamed, OldPath: "a_test.go", Path: "b_test.go"}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := f.Relevant(tt.change, tt.whitespaceOnly); got != tt.want {
				t.Errorf("Relevant(%+v, %v) = %v, want %v", tt.change, tt.whitespaceOnly, got, tt.want)
			}
		})
	}
}

func TestNeedsContentCheck(t *testing.T) {
	f := NewFilter(config.DefaultIgnore())
	if !f.NeedsContentCheck(gitx.Change{Status: gitx.StatusModified, Path: "a.go"}) {
		t.Error("a modified relevant file needs a content check")
	}
	if f.NeedsContentCheck(gitx.Change{Status: gitx.StatusModified, Path: "a_test.go"}) {
		t.Error("an ignored file needs no content check")
	}
	if f.NeedsContentCheck(gitx.Change{Status: gitx.StatusAdded, Path: "a.go"}) {
		t.Error("additions need no content check")
	}
}
