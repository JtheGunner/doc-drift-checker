package links

import "testing"

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		glob, name string
		want       bool
	}{
		{"main.go", "main.go", true},
		{"main.go", "cmd/main.go", false},
		{"*.go", "main.go", true},
		{"*.go", "cmd/main.go", false},
		{"cmd/*.go", "cmd/main.go", true},
		{"cmd/*.go", "cmd/sub/main.go", false},
		{"**/*.go", "main.go", true},
		{"**/*.go", "a/b/c/main.go", true},
		{"**/*.go", "a/b/main.md", false},
		{"internal/**", "internal/a/b.go", true},
		{"internal/**", "internal", false},
		{"internal/**", "other/a.go", false},
		{"internal/**/x.go", "internal/x.go", true},
		{"internal/**/x.go", "internal/a/b/x.go", true},
		{"internal/**/x.go", "internal/a/b/y.go", false},
		{"**", "any/thing.txt", true},
		{"a/**/b/**/c", "a/b/c", true},
		{"a/**/b/**/c", "a/x/b/y/z/c", true},
		{"a/**/b/**/c", "a/x/y/c", false},
		{"file?.txt", "file1.txt", true},
		{"file?.txt", "file12.txt", false},
		{"[ab].txt", "a.txt", true},
		{"[ab].txt", "c.txt", false},
		{"src**", "srcx/y", false},
		{"[", "[", false},
	}
	for _, tt := range tests {
		if got := matchGlob(tt.glob, tt.name); got != tt.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", tt.glob, tt.name, got, tt.want)
		}
	}
}
