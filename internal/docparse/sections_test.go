package docparse

import (
	"reflect"
	"testing"
)

func TestParseSections(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []Section
	}{
		{
			name: "empty input",
			src:  "",
			want: nil,
		},
		{
			name: "text without headings becomes a level 0 preamble",
			src:  "just text\nmore text\n",
			want: []Section{
				{Title: "", Anchor: "", Level: 0, StartLine: 1, EndLine: 2, Text: "just text\nmore text"},
			},
		},
		{
			name: "text before first heading",
			src:  "intro\n\n# Title\nbody\n",
			want: []Section{
				{Title: "", Anchor: "", Level: 0, StartLine: 1, EndLine: 1, Text: "intro"},
				{Title: "Title", Anchor: "title", Level: 1, StartLine: 3, EndLine: 4, Text: "# Title\nbody"},
			},
		},
		{
			name: "children belong to the parent range",
			src:  "# A\na\n## B\nb\n### C\nc\n## D\nd\n# E\ne\n",
			want: []Section{
				{Title: "A", Anchor: "a", Level: 1, StartLine: 1, EndLine: 8, Text: "# A\na\n## B\nb\n### C\nc\n## D\nd"},
				{Title: "B", Anchor: "b", Level: 2, StartLine: 3, EndLine: 6, Text: "## B\nb\n### C\nc"},
				{Title: "C", Anchor: "c", Level: 3, StartLine: 5, EndLine: 6, Text: "### C\nc"},
				{Title: "D", Anchor: "d", Level: 2, StartLine: 7, EndLine: 8, Text: "## D\nd"},
				{Title: "E", Anchor: "e", Level: 1, StartLine: 9, EndLine: 10, Text: "# E\ne"},
			},
		},
		{
			name: "hash inside code fence is not a heading",
			src:  "# Real\n```sh\n# comment\n```\n~~~\n## also not\n~~~\n",
			want: []Section{
				{Title: "Real", Anchor: "real", Level: 1, StartLine: 1, EndLine: 7, Text: "# Real\n```sh\n# comment\n```\n~~~\n## also not\n~~~"},
			},
		},
		{
			name: "setext headings",
			src:  "Title\n=====\ntext\n\nSub\n---\nmore\n",
			want: []Section{
				{Title: "Title", Anchor: "title", Level: 1, StartLine: 1, EndLine: 7, Text: "Title\n=====\ntext\n\nSub\n---\nmore"},
				{Title: "Sub", Anchor: "sub", Level: 2, StartLine: 5, EndLine: 7, Text: "Sub\n---\nmore"},
			},
		},
		{
			name: "duplicate titles get unique anchors",
			src:  "# Usage\na\n# Usage\nb\n# Usage\nc\n",
			want: []Section{
				{Title: "Usage", Anchor: "usage", Level: 1, StartLine: 1, EndLine: 2, Text: "# Usage\na"},
				{Title: "Usage", Anchor: "usage-1", Level: 1, StartLine: 3, EndLine: 4, Text: "# Usage\nb"},
				{Title: "Usage", Anchor: "usage-2", Level: 1, StartLine: 5, EndLine: 6, Text: "# Usage\nc"},
			},
		},
		{
			name: "anchor follows github slug rules and inline markup is stripped",
			src:  "## Install `go` & Run: v1.2!\n",
			want: []Section{
				{Title: "Install go & Run: v1.2!", Anchor: "install-go--run-v12", Level: 2, StartLine: 1, EndLine: 1, Text: "## Install `go` & Run: v1.2!"},
			},
		},
		{
			name: "trailing blank lines are excluded and CRLF is handled",
			src:  "# A\r\ntext\r\n\r\n\r\n# B\r\n",
			want: []Section{
				{Title: "A", Anchor: "a", Level: 1, StartLine: 1, EndLine: 2, Text: "# A\ntext"},
				{Title: "B", Anchor: "b", Level: 1, StartLine: 5, EndLine: 5, Text: "# B"},
			},
		},
		{
			name: "skipped heading levels nest by level number",
			src:  "# A\n### C\nc\n## B\nb\n",
			want: []Section{
				{Title: "A", Anchor: "a", Level: 1, StartLine: 1, EndLine: 5, Text: "# A\n### C\nc\n## B\nb"},
				{Title: "C", Anchor: "c", Level: 3, StartLine: 2, EndLine: 3, Text: "### C\nc"},
				{Title: "B", Anchor: "b", Level: 2, StartLine: 4, EndLine: 5, Text: "## B\nb"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseSections([]byte(tt.src))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseSections() =\n%#v\nwant\n%#v", got, tt.want)
			}
		})
	}
}
