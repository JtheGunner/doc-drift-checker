package textsafe

import (
	"strings"
	"testing"
)

func TestEscape(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		limit int
		want  string
	}{
		{"plain", "hello world", 200, "hello world"},
		{"empty", "", 200, ""},
		{"newline and tab", "a\nb\tc", 200, `a\x0ab\x09c`},
		{"escape sequence", "\x1b[31mred", 200, `\x1b[31mred`},
		{"nul", "a\x00b", 200, `a\x00b`},
		{"c1 control", "a\u0085b", 200, `a\x85b`},
		{"zero width space", "a\u200bb", 200, "a\\u200bb"},
		{"bidi override", "a\u202eb", 200, "a\\u202eb"},
		{"line separator", "a\u2028b", 200, "a\\u2028b"},
		{"paragraph separator", "a\u2029b", 200, "a\\u2029b"},
		{"invalid utf8", "a\xffb", 200, "a\ufffdb"},
		{"unicode kept", "grüße 日本", 200, "grüße 日本"},
		{"exactly at limit", "abc", 3, "abc"},
		{"over limit", "abcd", 3, "abc..."},
		{"limit counts characters", "日本語x", 3, "日本語..."},
		{"escape counts as one", "\n\n\nx", 3, `\x0a\x0a\x0a...`},
		{"zero limit", "a", 0, "..."},
		{"zero limit empty", "", 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Escape(tt.in, tt.limit); got != tt.want {
				t.Errorf("Escape(%q, %d) = %q, want %q", tt.in, tt.limit, got, tt.want)
			}
		})
	}
}

func TestMask(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		limit int
		want  string
	}{
		{"plain with spaces", "a heading", 80, "a heading"},
		{"empty", "", 80, ""},
		{"newline", "a\nb", 80, "a?b"},
		{"escape", "\x1b[0m", 80, "?[0m"},
		{"format char", "a\u200bb", 80, "a?b"},
		{"line separator", "a\u2028b", 80, "a?b"},
		{"non-breaking space", "a\u00a0b", 80, "a?b"},
		{"invalid utf8 kept as replacement", "a\xffb", 80, "a\ufffdb"},
		{"unicode kept", "grüße 日本", 80, "grüße 日本"},
		{"exactly at limit", "abc", 3, "abc"},
		{"over limit", "abcd", 3, "abc..."},
		{"limit counts characters", "日本語x", 3, "日本語..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Mask(tt.in, tt.limit); got != tt.want {
				t.Errorf("Mask(%q, %d) = %q, want %q", tt.in, tt.limit, got, tt.want)
			}
		})
	}
}

func TestOutputHasNoRawControlCharacters(t *testing.T) {
	var in strings.Builder
	for r := rune(0); r < 0x200; r++ {
		in.WriteRune(r)
	}
	outputs := map[string]string{
		"Escape": Escape(in.String(), 1000),
		"Mask":   Mask(in.String(), 1000),
	}
	for name, got := range outputs {
		for _, r := range got {
			if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
				t.Errorf("%s left control character %U", name, r)
			}
		}
	}
}
