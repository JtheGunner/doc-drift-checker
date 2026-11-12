package relevance

import (
	"errors"
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/gitx"
)

func TestMatchingGlob(t *testing.T) {
	f := NewFilter([]string{"vendor/**", "**/*_test.go"})
	tests := []struct {
		path     string
		wantGlob string
		wantOK   bool
	}{
		{"vendor/a/b.go", "vendor/**", true},
		{"pkg/x_test.go", "**/*_test.go", true},
		{"pkg/x.go", "", false},
	}
	for _, tt := range tests {
		glob, ok := f.MatchingGlob(tt.path)
		if glob != tt.wantGlob || ok != tt.wantOK {
			t.Errorf("MatchingGlob(%q) = %q, %v; want %q, %v", tt.path, glob, ok, tt.wantGlob, tt.wantOK)
		}
		if f.Ignored(tt.path) != tt.wantOK {
			t.Errorf("Ignored(%q) disagrees with MatchingGlob", tt.path)
		}
	}
}

func probeOf(whitespace bool, oldSrc, newSrc string) Probe {
	return Probe{
		WhitespaceOnly: func() (bool, error) { return whitespace, nil },
		Sources:        func() ([]byte, []byte, error) { return []byte(oldSrc), []byte(newSrc), nil },
	}
}

func TestDecide(t *testing.T) {
	filter := NewFilter([]string{"**/*_test.go", "gen/**"})
	analyzers := NewAnalyzerSet(DefaultAnalyzers()...)
	const apiOld = "package p\n\nfunc A() {}\n"
	tests := []struct {
		name   string
		change gitx.Change
		probe  Probe
		want   Decision
	}{
		{
			name:   "ignored path names the glob",
			change: gitx.Change{Status: gitx.StatusModified, Path: "pkg/a_test.go"},
			probe:  probeOf(false, apiOld, apiOld),
			want:   Decision{Relevant: false, Reason: ReasonIgnoredPath, Glob: "**/*_test.go"},
		},
		{
			name:   "rename within ignored paths names both globs",
			change: gitx.Change{Status: gitx.StatusRenamed, Path: "gen/b.go", OldPath: "pkg/a_test.go"},
			probe:  probeOf(false, apiOld, apiOld),
			want:   Decision{Relevant: false, Reason: ReasonIgnoredPath, Glob: "gen/**", OldGlob: "**/*_test.go"},
		},
		{
			name:   "rename into ignored path stays relevant",
			change: gitx.Change{Status: gitx.StatusRenamed, Path: "pkg/a_test.go", OldPath: "pkg/a.go"},
			probe:  probeOf(false, apiOld, apiOld),
			want:   Decision{Relevant: true, Reason: ReasonRenameAcrossIgnore, Glob: "**/*_test.go"},
		},
		{
			name:   "rename out of ignored path stays relevant",
			change: gitx.Change{Status: gitx.StatusRenamed, Path: "pkg/a.go", OldPath: "gen/a.go"},
			probe:  probeOf(false, apiOld, apiOld),
			want:   Decision{Relevant: true, Reason: ReasonRenameAcrossIgnore, OldGlob: "gen/**"},
		},
		{
			name:   "plain rename is never dropped",
			change: gitx.Change{Status: gitx.StatusRenamed, Path: "pkg/b.go", OldPath: "pkg/a.go"},
			probe:  probeOf(false, apiOld, apiOld),
			want:   Decision{Relevant: true, Reason: ReasonPathBased},
		},
		{
			name:   "whitespace-only modification is dropped",
			change: gitx.Change{Status: gitx.StatusModified, Path: "script.py"},
			probe:  probeOf(true, "", ""),
			want:   Decision{Relevant: false, Reason: ReasonWhitespaceOnly},
		},
		{
			name:   "unchanged Go API is dropped and names the analyzer",
			change: gitx.Change{Status: gitx.StatusModified, Path: "pkg/a.go"},
			probe:  probeOf(false, apiOld, "package p\n\nfunc A() { _ = 1 }\n"),
			want:   Decision{Relevant: false, Reason: ReasonAPIUnchanged, Analyzer: ".go"},
		},
		{
			name:   "changed Go API stays relevant",
			change: gitx.Change{Status: gitx.StatusModified, Path: "pkg/a.go"},
			probe:  probeOf(false, apiOld, "package p\n\nfunc A(n int) {}\n"),
			want:   Decision{Relevant: true, Reason: ReasonAPIChanged, Analyzer: ".go"},
		},
		{
			name:   "unparsable Go source fails closed",
			change: gitx.Change{Status: gitx.StatusModified, Path: "pkg/a.go"},
			probe:  probeOf(false, apiOld, "package p\n\nfunc {"),
			want:   Decision{Relevant: true, Reason: ReasonAPIChanged, Analyzer: ".go"},
		},
		{
			name:   "language without analyzer falls back to the path",
			change: gitx.Change{Status: gitx.StatusModified, Path: "script.py"},
			probe:  probeOf(false, "", ""),
			want:   Decision{Relevant: true, Reason: ReasonNoAnalyzer},
		},
		{
			name:   "type change is path-based",
			change: gitx.Change{Status: gitx.StatusTypeChanged, Path: "pkg/a.go"},
			probe:  probeOf(false, apiOld, apiOld),
			want:   Decision{Relevant: true, Reason: ReasonPathBased},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := filter.Decide(analyzers, tt.change, tt.probe)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("Decide = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestDecideAnalyzerFailureIsReportedAndKept(t *testing.T) {
	failing := &fakeAnalyzer{exts: []string{".txt"}, err: errors.New("boom")}
	got, err := NewFilter(nil).Decide(NewAnalyzerSet(failing), gitx.Change{Status: gitx.StatusModified, Path: "n.txt"}, probeOf(false, "a", "b"))
	if err != nil {
		t.Fatal(err)
	}
	want := Decision{Relevant: true, Reason: ReasonAnalyzerFailed, Analyzer: ".txt"}
	if got != want {
		t.Fatalf("Decide = %+v, want %+v", got, want)
	}
}

func TestDecideOnlyProbesWhatIsNeeded(t *testing.T) {
	filter := NewFilter([]string{"ignored/**"})
	analyzers := NewAnalyzerSet(DefaultAnalyzers()...)
	probe := Probe{
		WhitespaceOnly: func() (bool, error) { t.Fatal("whitespace probe must not run"); return false, nil },
		Sources:        func() ([]byte, []byte, error) { t.Fatal("source probe must not run"); return nil, nil, nil },
	}
	cases := []gitx.Change{
		{Status: gitx.StatusModified, Path: "ignored/a.go"},
		{Status: gitx.StatusAdded, Path: "ignored/a.go"},
		{Status: gitx.StatusRenamed, Path: "x/a.go", OldPath: "x/b.go"},
	}
	for _, c := range cases {
		if _, err := filter.Decide(analyzers, c, probe); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDecidePropagatesProbeErrors(t *testing.T) {
	analyzers := NewAnalyzerSet(DefaultAnalyzers()...)
	boom := errors.New("boom")
	failing := Probe{
		WhitespaceOnly: func() (bool, error) { return false, boom },
		Sources:        func() ([]byte, []byte, error) { return nil, nil, boom },
	}
	for _, c := range []gitx.Change{
		{Status: gitx.StatusModified, Path: "a.py"},
		{Status: gitx.StatusAdded, Path: "a.go"},
	} {
		if _, err := NewFilter(nil).Decide(analyzers, c, failing); !errors.Is(err, boom) {
			t.Errorf("Decide(%+v) error = %v, want boom", c, err)
		}
	}
}
