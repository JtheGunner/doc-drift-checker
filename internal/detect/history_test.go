package detect_test

import (
	"reflect"
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/detect"
)

func TestOwnRange(t *testing.T) {
	d := docs()[0].Sections
	tests := []struct {
		index      int
		start, end int
	}{
		{0, 1, 2}, // "Project" ends before its first child
		{1, 3, 6}, // "Usage" is cut before its child "Flags"
		{2, 7, 10},
		{3, 11, 12},
	}
	for _, tt := range tests {
		start, end := detect.OwnRange(d, tt.index)
		if start != tt.start || end != tt.end {
			t.Errorf("OwnRange(%d) = %d-%d, want %d-%d", tt.index, start, end, tt.start, tt.end)
		}
	}
}

func TestDetectHistory(t *testing.T) {
	usage := detect.SectionKey{Doc: "README.md", Line: 3}
	flags := detect.SectionKey{Doc: "README.md", Line: 7}
	other := detect.SectionKey{Doc: "README.md", Line: 11}
	tests := []struct {
		name     string
		sections map[detect.SectionKey]int64
		files    map[string]int64
		want     []detect.Drift
	}{
		{name: "no dates"},
		{
			name:     "code newer than section",
			sections: map[detect.SectionKey]int64{usage: 100},
			files:    map[string]int64{"a.go": 200},
			want:     []detect.Drift{{Doc: "README.md", Section: "Usage", Line: 3, Files: []string{"a.go"}}},
		},
		{
			name:     "section newer than code",
			sections: map[detect.SectionKey]int64{usage: 300},
			files:    map[string]int64{"a.go": 200},
		},
		{
			name:     "equal timestamps are not drift",
			sections: map[detect.SectionKey]int64{usage: 200},
			files:    map[string]int64{"a.go": 200},
		},
		{
			name:  "section without date is not reported",
			files: map[string]int64{"a.go": 200},
		},
		{
			name:     "linked file without date is not considered",
			sections: map[detect.SectionKey]int64{usage: 100},
			files:    map[string]int64{"unrelated.go": 200},
		},
		{
			name:     "only newer files trigger",
			sections: map[detect.SectionKey]int64{usage: 100, flags: 100, other: 100},
			files:    map[string]int64{"a.go": 150, "b.go": 90, "c.go": 150},
			want: []detect.Drift{
				{Doc: "README.md", Section: "Usage", Line: 3, Files: []string{"a.go"}},
				{Doc: "README.md", Section: "Other", Line: 11, Files: []string{"c.go"}},
			},
		},
		{
			name:     "sections are dated independently",
			sections: map[detect.SectionKey]int64{usage: 500, flags: 100},
			files:    map[string]int64{"a.go": 200, "b.go": 200},
			want:     []detect.Drift{{Doc: "README.md", Section: "Flags", Line: 7, Files: []string{"b.go"}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detect.DetectHistory(detect.HistoryInput{Docs: docs(), SectionDates: tt.sections, FileDates: tt.files})
			if tt.want == nil {
				tt.want = []detect.Drift{}
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}
