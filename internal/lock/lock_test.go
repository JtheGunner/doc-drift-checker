package lock_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/lock"
)

var (
	hashA = lock.Hash([]byte("a"))
	hashB = lock.Hash([]byte("b"))
)

func TestHash(t *testing.T) {
	const want = "sha256:ca978112ca1bbdcafac231b39a23dc4da786eff8147c4e72b9807785afee48bb"
	if got := lock.Hash([]byte("a")); got != want {
		t.Fatalf("Hash = %q, want %q", got, want)
	}
}

func TestMarshalGolden(t *testing.T) {
	l := lock.Lock{Sections: map[string]map[string]string{
		"b.md#two":       {"z.go": hashB, "a.go": hashA},
		"README.md#home": {"src/main.go": hashA},
	}}
	got, err := l.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "version": 1,
  "sections": {
    "README.md#home": {
      "files": {
        "src/main.go": "` + hashA + `"
      }
    },
    "b.md#two": {
      "files": {
        "a.go": "` + hashA + `",
        "z.go": "` + hashB + `"
      }
    }
  }
}
`
	if string(got) != want {
		t.Fatalf("Marshal =\n%s\nwant\n%s", got, want)
	}
}

func TestMarshalEmpty(t *testing.T) {
	got, err := lock.Lock{}.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\n  \"version\": 1,\n  \"sections\": {}\n}\n"; string(got) != want {
		t.Fatalf("Marshal = %q, want %q", got, want)
	}
}

func TestMarshalDoesNotEscapeHTML(t *testing.T) {
	l := lock.Lock{Sections: map[string]map[string]string{"R.md#a": {"a&b<c>.go": hashA}}}
	got, err := l.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"a&b<c>.go"`) {
		t.Fatalf("path was escaped:\n%s", got)
	}
}

func TestMarshalIsDeterministic(t *testing.T) {
	first := lock.Lock{Sections: map[string]map[string]string{}}
	second := lock.Lock{Sections: map[string]map[string]string{}}
	keys := []string{"a.md#x", "b.md#y", "c.md#z", "d.md#w"}
	for i, key := range keys {
		first.Sections[key] = map[string]string{"f1.go": hashA, "f2.go": hashB}
		other := keys[len(keys)-1-i]
		second.Sections[other] = map[string]string{"f2.go": hashB, "f1.go": hashA}
	}
	a, err := first.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		b, err := second.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		if string(a) != string(b) {
			t.Fatalf("bytes differ:\n%s\n%s", a, b)
		}
	}
}

func TestParseRoundTrip(t *testing.T) {
	l := lock.Lock{Sections: map[string]map[string]string{
		"README.md#home": {"a.go": hashA, "dir/b.go": hashB},
		"docs/x.md#y-1":  {"c.go": hashA},
	}}
	data, err := l.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	got, err := lock.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, l) {
		t.Fatalf("Parse = %v, want %v", got, l)
	}
	again, err := got.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(data) {
		t.Fatalf("round trip changed bytes:\n%s\n%s", data, again)
	}
}

func TestParseEmptySections(t *testing.T) {
	got, err := lock.Parse([]byte(`{"version":1,"sections":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sections) != 0 {
		t.Fatalf("Sections = %v", got.Sections)
	}
}

func TestParseRejects(t *testing.T) {
	entry := func(files string) string {
		return `{"version":1,"sections":{"R.md#a":{"files":` + files + `}}}`
	}
	file := func(path string) string { return entry(`{"` + path + `":"` + hashA + `"}`) }
	tests := []struct {
		name string
		in   string
	}{
		{"empty input", ``},
		{"garbage", `not json`},
		{"array", `[]`},
		{"null", `null`},
		{"trailing data", `{"version":1,"sections":{}} {}`},
		{"trailing garbage", `{"version":1,"sections":{}}x`},
		{"missing version", `{"sections":{}}`},
		{"missing sections", `{"version":1}`},
		{"wrong version", `{"version":2,"sections":{}}`},
		{"string version", `{"version":"1","sections":{}}`},
		{"float version", `{"version":1.0,"sections":{}}`},
		{"unknown top-level field", `{"version":1,"sections":{},"extra":1}`},
		{"duplicate top-level field", `{"version":1,"version":1,"sections":{}}`},
		{"sections not an object", `{"version":1,"sections":[]}`},
		{"unknown section field", `{"version":1,"sections":{"R.md#a":{"files":{"a.go":"` + hashA + `"},"note":"x"}}}`},
		{"missing files", `{"version":1,"sections":{"R.md#a":{}}}`},
		{"files not an object", entry(`["a.go"]`)},
		{"empty files", entry(`{}`)},
		{"empty section key", `{"version":1,"sections":{"":{"files":{"a.go":"` + hashA + `"}}}}`},
		{"section key without anchor separator", `{"version":1,"sections":{"README":{"files":{"a.go":"` + hashA + `"}}}}`},
		{"section key without doc", `{"version":1,"sections":{"#a":{"files":{"a.go":"` + hashA + `"}}}}`},
		{"section key without anchor", `{"version":1,"sections":{"R.md#":{"files":{"a.go":"` + hashA + `"}}}}`},
		{"duplicate section key", `{"version":1,"sections":{"R.md#a":{"files":{"a.go":"` + hashA + `"}},"R.md#a":{"files":{"a.go":"` + hashA + `"}}}}`},
		{"duplicate file path", entry(`{"a.go":"` + hashA + `","a.go":"` + hashA + `"}`)},
		{"empty path", file("")},
		{"absolute path", file("/etc/passwd")},
		{"parent traversal", file("../x.go")},
		{"embedded traversal", file("a/../../x.go")},
		{"dot segment", file("./a.go")},
		{"unclean double slash", file("a//b.go")},
		{"trailing slash", file("a/")},
		{"backslash", file(`a\\b.go`)},
		{"drive letter", file("C:/x.go")},
		{"control character", file(`a\u0001b.go`)},
		{"newline in path", file(`a\nb.go`)},
		{"hash without prefix", entry(`{"a.go":"` + strings.TrimPrefix(hashA, "sha256:") + `"}`)},
		{"hash too short", entry(`{"a.go":"sha256:abcd"}`)},
		{"hash uppercase", entry(`{"a.go":"sha256:` + strings.Repeat("A", 64) + `"}`)},
		{"hash non-hex", entry(`{"a.go":"sha256:` + strings.Repeat("g", 64) + `"}`)},
		{"hash not a string", entry(`{"a.go":1}`)},
		{"other algorithm", entry(`{"a.go":"md5:` + strings.Repeat("a", 64) + `"}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, err := lock.Parse([]byte(tt.in)); err == nil {
				t.Fatalf("Parse accepted %q: %v", tt.in, got)
			}
		})
	}
}

func TestParseRejectsOversize(t *testing.T) {
	data := []byte(`{"version":1,"sections":{}}` + strings.Repeat(" ", lock.MaxBytes))
	if _, err := lock.Parse(data); err == nil {
		t.Fatal("Parse accepted an oversized lockfile")
	}
}

func TestParseAcceptsUnusualButSafeNames(t *testing.T) {
	in := `{"version":1,"sections":{"a#b.md#c":{"files":{"dir/.hidden":"` + hashA + `","..x/y..go":"` + hashA + `"}}}}`
	if _, err := lock.Parse([]byte(in)); err != nil {
		t.Fatalf("Parse rejected safe names: %v", err)
	}
}

func TestWithReplacesOnlyThatSection(t *testing.T) {
	l := lock.Lock{Sections: map[string]map[string]string{
		"R.md#a": {"a.go": hashA},
		"R.md#b": {"b.go": hashA},
	}}
	before, err := l.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	next := l.With("R.md#b", map[string]string{"b.go": hashB, "c.go": hashA})
	if !reflect.DeepEqual(next.Sections["R.md#a"], l.Sections["R.md#a"]) {
		t.Fatal("other section changed")
	}
	if got := next.Sections["R.md#b"]; got["b.go"] != hashB || got["c.go"] != hashA {
		t.Fatalf("section not replaced: %v", got)
	}
	after, err := l.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("With mutated the receiver")
	}
	fresh := lock.Lock{}.With("R.md#a", map[string]string{"a.go": hashA})
	if len(fresh.Sections) != 1 {
		t.Fatalf("With on empty lock = %v", fresh)
	}
}

func TestDiff(t *testing.T) {
	tests := []struct {
		name            string
		locked, current map[string]string
		want            []string
	}{
		{"equal", map[string]string{"a": hashA, "b": hashB}, map[string]string{"b": hashB, "a": hashA}, []string{}},
		{"changed hash", map[string]string{"a": hashA}, map[string]string{"a": hashB}, []string{"a"}},
		{"added file", map[string]string{"a": hashA}, map[string]string{"a": hashA, "b": hashB}, []string{"b"}},
		{"removed file", map[string]string{"a": hashA, "b": hashB}, map[string]string{"a": hashA}, []string{"b"}},
		{"all kinds sorted", map[string]string{"z": hashA, "m": hashA}, map[string]string{"m": hashB, "a": hashA}, []string{"a", "m", "z"}},
		{"both empty", nil, nil, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lock.Diff(tt.locked, tt.current); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Diff = %v, want %v", got, tt.want)
			}
		})
	}
}
