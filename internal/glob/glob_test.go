package glob

import (
	"strings"
	"testing"
	"time"
)

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
		if got := Match(tt.glob, tt.name); got != tt.want {
			t.Errorf("Match(%q, %q) = %v, want %v", tt.glob, tt.name, got, tt.want)
		}
	}
}

func TestMatchGlobConsecutiveDoubleStars(t *testing.T) {
	if !Match("a/**/**/b", "a/b") || !Match("a/**/**/b", "a/x/y/b") || Match("a/**/**/b", "a/x/y/c") {
		t.Error("consecutive ** must behave like a single **")
	}
	if Match("a/**/**", "a") || !Match("a/**/**", "a/x") {
		t.Error("trailing ** run still requires at least one segment")
	}
}

func TestMatchGlobDoesNotBacktrackExponentially(t *testing.T) {
	pattern := strings.Repeat("**/", 20) + "needle.go"
	name := strings.Repeat("d/", 30) + "other.go"
	alternating := strings.Repeat("**/x/", 20) + "needle.go"

	done := make(chan [2]bool, 1)
	go func() {
		done <- [2]bool{Match(pattern, name), Match(alternating, strings.Repeat("x/", 40)+"other.go")}
	}()
	select {
	case got := <-done:
		if got[0] || got[1] {
			t.Errorf("non-matching patterns matched: %v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Match did not finish within 2s: exponential backtracking")
	}
	if !Match(pattern, strings.Repeat("d/", 30)+"needle.go") {
		t.Error("expected a match for the deep path ending in needle.go")
	}
}
