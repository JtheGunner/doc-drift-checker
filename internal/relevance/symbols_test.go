package relevance

import (
	"errors"
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/gitx"
)

// fakeAnalyzer proves that a language needs nothing but the interface.
type fakeAnalyzer struct {
	exts    []string
	changed bool
	err     error
	calls   int
}

func (f *fakeAnalyzer) Extensions() []string { return f.exts }

func (f *fakeAnalyzer) APIChanged(_, _ []byte) (bool, error) {
	f.calls++
	return f.changed, f.err
}

func TestAnalyzerFor(t *testing.T) {
	txt := &fakeAnalyzer{exts: []string{".txt"}}
	set := NewAnalyzerSet(txt)
	tests := []struct {
		path string
		want SymbolAnalyzer
	}{
		{"notes.txt", txt},
		{"dir/NOTES.TXT", txt},
		{"script.py", nil},
		{"Makefile", nil},
		{"dir.txt/file", nil},
		{".txt", txt},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := set.For(tt.path); got != tt.want {
				t.Errorf("For(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestAPIChangedFallbackAndConsultation(t *testing.T) {
	txt := &fakeAnalyzer{exts: []string{".txt"}, changed: false}
	set := NewAnalyzerSet(txt)
	mod := func(p string) gitx.Change { return gitx.Change{Status: gitx.StatusModified, Path: p} }

	// A new language is consulted without any other change.
	if set.Supports(mod("a.txt")) != true {
		t.Fatal("registered extension must be supported")
	}
	changed, err := set.APIChanged(mod("a.txt"), []byte("x"), []byte("y"))
	if err != nil || changed {
		t.Fatalf("APIChanged = %v, %v; want false, nil from the fake", changed, err)
	}
	if txt.calls != 1 {
		t.Fatalf("fake analyzer consulted %d times, want 1", txt.calls)
	}
	txt.changed = true
	if changed, _ := set.APIChanged(mod("a.txt"), nil, nil); !changed {
		t.Fatal("analyzer verdict must be passed through")
	}

	// Unsupported extension: fallback keeps the change relevant, analyzer untouched.
	txt.calls = 0
	if set.Supports(mod("a.py")) {
		t.Fatal(".py must not be supported")
	}
	changed, err = set.APIChanged(mod("a.py"), []byte("x"), []byte("y"))
	if err != nil || !changed || txt.calls != 0 {
		t.Fatalf("fallback = %v, %v, calls %d; want true, nil, 0", changed, err, txt.calls)
	}
}

func TestSupportsOnlyAddedModifiedDeleted(t *testing.T) {
	set := NewAnalyzerSet(&fakeAnalyzer{exts: []string{".txt"}})
	tests := []struct {
		status gitx.Status
		want   bool
	}{
		{gitx.StatusModified, true},
		{gitx.StatusAdded, true},
		{gitx.StatusDeleted, true},
		{gitx.StatusRenamed, false},
		{gitx.StatusCopied, false},
		{gitx.StatusTypeChanged, false},
	}
	for _, tt := range tests {
		c := gitx.Change{Status: tt.status, Path: "a.txt"}
		if got := set.Supports(c); got != tt.want {
			t.Errorf("Supports(status %s) = %v, want %v", tt.status, got, tt.want)
		}
	}
}

func TestAPIChangedErrorFromAnalyzerIsReportedAsChange(t *testing.T) {
	boom := &fakeAnalyzer{exts: []string{".txt"}, err: errors.New("boom")}
	changed, err := NewAnalyzerSet(boom).APIChanged(gitx.Change{Status: gitx.StatusModified, Path: "a.txt"}, nil, nil)
	if err != nil || !changed {
		t.Fatalf("got %v, %v; an analyzer error must keep the change relevant without failing the check", changed, err)
	}
}
