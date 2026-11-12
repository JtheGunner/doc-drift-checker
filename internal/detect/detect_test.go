package detect_test

import (
	"reflect"
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/detect"
	"github.com/JtheGunner/doc-drift-checker/internal/links"
)

// docs: "Usage" (3-10) links a.go and has child "Flags" (7-10) linking b.go.
func docs() []links.DocLinks {
	return []links.DocLinks{{Doc: "README.md", Sections: []links.SectionLinks{
		{Heading: "Project", Level: 1, StartLine: 1, EndLine: 12},
		{Heading: "Usage", Level: 2, StartLine: 3, EndLine: 10, Links: []links.Link{{File: "a.go"}}},
		{Heading: "Flags", Level: 3, StartLine: 7, EndLine: 10, Links: []links.Link{{File: "b.go"}}},
		{Heading: "Other", Level: 2, StartLine: 11, EndLine: 12, Links: []links.Link{{File: "c.go"}}},
	}}}
}

func TestDetect(t *testing.T) {
	tests := []struct {
		name    string
		changes []detect.Change
		docs    map[string][]detect.LineRange
		want    []detect.Drift
	}{
		{name: "nothing changed"},
		{
			name:    "code only",
			changes: []detect.Change{{Path: "a.go"}},
			want:    []detect.Drift{{Doc: "README.md", Section: "Usage", Line: 3, Files: []string{"a.go"}}},
		},
		{
			name:    "code and section changed",
			changes: []detect.Change{{Path: "a.go"}},
			docs:    map[string][]detect.LineRange{"README.md": {{Start: 4, Lines: 1}}},
		},
		{
			name:    "doc only changed",
			docs:    map[string][]detect.LineRange{"README.md": {{Start: 4, Lines: 1}}},
			changes: []detect.Change{{Path: "unrelated.go"}},
		},
		{
			name:    "unlinked README change is not code",
			changes: []detect.Change{{Path: "README.md"}},
		},
		{
			name:    "child edit does not hide parent drift",
			changes: []detect.Change{{Path: "a.go"}},
			docs:    map[string][]detect.LineRange{"README.md": {{Start: 8, Lines: 1}}},
			want:    []detect.Drift{{Doc: "README.md", Section: "Usage", Line: 3, Files: []string{"a.go"}}},
		},
		{
			name:    "parent edit does not hide child drift",
			changes: []detect.Change{{Path: "b.go"}},
			docs:    map[string][]detect.LineRange{"README.md": {{Start: 4, Lines: 1}}},
			want:    []detect.Drift{{Doc: "README.md", Section: "Flags", Line: 7, Files: []string{"b.go"}}},
		},
		{
			name:    "edit in another section does not hide drift",
			changes: []detect.Change{{Path: "c.go"}},
			docs:    map[string][]detect.LineRange{"README.md": {{Start: 4, Lines: 1}}},
			want:    []detect.Drift{{Doc: "README.md", Section: "Other", Line: 11, Files: []string{"c.go"}}},
		},
		{
			name:    "deletion inside section counts as touch",
			changes: []detect.Change{{Path: "a.go"}},
			docs:    map[string][]detect.LineRange{"README.md": {{Start: 5, Lines: 0}}},
		},
		{
			name:    "deletion before heading belongs to previous section",
			changes: []detect.Change{{Path: "c.go"}},
			docs:    map[string][]detect.LineRange{"README.md": {{Start: 9, Lines: 0}}},
			want:    []detect.Drift{{Doc: "README.md", Section: "Other", Line: 11, Files: []string{"c.go"}}},
		},
		{
			name:    "range overlapping section start touches it",
			changes: []detect.Change{{Path: "a.go"}},
			docs:    map[string][]detect.LineRange{"README.md": {{Start: 1, Lines: 3}}},
		},
		{
			name:    "renamed file matches by new path",
			changes: []detect.Change{{Path: "a.go", OldPath: "old/a.go"}},
			want:    []detect.Drift{{Doc: "README.md", Section: "Usage", Line: 3, Files: []string{"old/a.go -> a.go"}}},
		},
		{
			name:    "renamed file matches by old path",
			changes: []detect.Change{{Path: "moved/a.go", OldPath: "a.go"}},
			want:    []detect.Drift{{Doc: "README.md", Section: "Usage", Line: 3, Files: []string{"a.go -> moved/a.go"}}},
		},
		{
			name:    "files are sorted",
			changes: []detect.Change{{Path: "b.go"}, {Path: "a.go"}},
			want: []detect.Drift{
				{Doc: "README.md", Section: "Usage", Line: 3, Files: []string{"a.go"}},
				{Doc: "README.md", Section: "Flags", Line: 7, Files: []string{"b.go"}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detect.Detect(detect.Input{Docs: docs(), Changes: tt.changes, DocChanges: tt.docs})
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}
