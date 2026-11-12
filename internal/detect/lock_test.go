package detect_test

import (
	"github.com/JtheGunner/doc-drift-checker/internal/docparse"
	"reflect"
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/detect"
)

func TestApplyLock(t *testing.T) {
	usage := detect.SectionKey{Doc: "README.md", Line: 3}
	flags := detect.SectionKey{Doc: "README.md", Line: 7}
	other := detect.SectionKey{Doc: "README.md", Line: 11}
	drift := func(key detect.SectionKey, section string, files ...string) detect.Drift {
		return detect.Drift{Doc: key.Doc, Section: section, Line: key.Line, Files: files, Severity: "low", BrokenReferences: []docparse.Reference{}}
	}
	tests := []struct {
		name    string
		history bool
		drifts  []detect.Drift
		locked  map[detect.SectionKey][]string
		want    []detect.Drift
	}{
		{name: "no entries leave drifts alone", drifts: []detect.Drift{drift(usage, "Usage", "a.go")}, want: []detect.Drift{drift(usage, "Usage", "a.go")}},
		{name: "diff: matching state suppresses", drifts: []detect.Drift{drift(usage, "Usage", "a.go")}, locked: map[detect.SectionKey][]string{usage: {}}, want: []detect.Drift{}},
		{name: "diff: mismatch keeps the normal drift", drifts: []detect.Drift{drift(usage, "Usage", "a.go")}, locked: map[detect.SectionKey][]string{usage: {"b.go"}}, want: []detect.Drift{drift(usage, "Usage", "a.go")}},
		{name: "diff: mismatch without drift adds none", locked: map[detect.SectionKey][]string{usage: {"b.go"}}, want: []detect.Drift{}},
		{name: "diff: other sections unaffected", drifts: []detect.Drift{drift(usage, "Usage", "a.go"), drift(other, "Other", "o.go")}, locked: map[detect.SectionKey][]string{usage: {}}, want: []detect.Drift{drift(other, "Other", "o.go")}},
		{name: "history: matching state suppresses dated drift", history: true, drifts: []detect.Drift{drift(usage, "Usage", "a.go")}, locked: map[detect.SectionKey][]string{usage: {}}, want: []detect.Drift{}},
		{name: "history: mismatch reports without a dated drift", history: true, locked: map[detect.SectionKey][]string{usage: {"b.go", "a.go"}}, want: []detect.Drift{drift(usage, "Usage", "b.go", "a.go")}},
		{name: "history: mismatch lists the changed files", history: true, drifts: []detect.Drift{drift(usage, "Usage", "a.go")}, locked: map[detect.SectionKey][]string{usage: {"b.go"}}, want: []detect.Drift{drift(usage, "Usage", "b.go")}},
		{name: "history: document order and unlocked sections kept", history: true,
			drifts: []detect.Drift{drift(flags, "Flags", "f.go"), drift(other, "Other", "o.go")},
			locked: map[detect.SectionKey][]string{usage: {"a.go"}, other: {}},
			want:   []detect.Drift{drift(usage, "Usage", "a.go"), drift(flags, "Flags", "f.go")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detect.ApplyLock(docs(), tt.drifts, tt.locked, tt.history)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ApplyLock = %+v, want %+v", got, tt.want)
			}
		})
	}
}
