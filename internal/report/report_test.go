package report_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/config"
	"github.com/JtheGunner/doc-drift-checker/internal/detect"
	"github.com/JtheGunner/doc-drift-checker/internal/docparse"
	"github.com/JtheGunner/doc-drift-checker/internal/report"
)

var sample = []detect.Drift{
	{
		Doc: "README.md", Section: "Usage", Line: 3, Files: []string{"a.go", "old.go -> new.go"},
		Severity: config.SeverityHigh,
		BrokenReferences: []docparse.Reference{
			{Kind: docparse.KindFlag, Value: "--gone", Line: 5},
			{Kind: docparse.KindEnvVar, Value: "APP_TOKEN", Line: 6},
		},
	},
	{Doc: "docs/x.md", Section: "Flags", Line: 9, Files: []string{"b.go"}, Severity: config.SeverityLow},
}

func TestWriteText(t *testing.T) {
	var buf bytes.Buffer
	if err := report.WriteText(&buf, sample); err != nil {
		t.Fatal(err)
	}
	want := "[high] Usage\n  README.md:3\n  changed code:\n    a.go\n    old.go -> new.go\n" +
		"  broken references:\n    flag --gone (README.md:5)\n    env-var APP_TOKEN (README.md:6)\n" +
		"[low] Flags\n  docs/x.md:9\n  changed code:\n    b.go\n"
	if buf.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", buf.String(), want)
	}
}

func TestWriteTextEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := report.WriteText(&buf, nil); err != nil || buf.Len() != 0 {
		t.Fatalf("want empty output, got %q (%v)", buf.String(), err)
	}
}

func TestWriteJSONHistoryMode(t *testing.T) {
	var buf bytes.Buffer
	if err := report.WriteJSON(&buf, report.ModeHistory, "", nil); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"schemaVersion\": 1,\n  \"mode\": \"history\",\n  \"base\": \"\",\n  \"drifts\": []\n}\n"
	if buf.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", buf.String(), want)
	}
}

func TestWriteJSONGolden(t *testing.T) {
	var buf bytes.Buffer
	if err := report.WriteJSON(&buf, report.ModeDiff, "main", sample[:1]); err != nil {
		t.Fatal(err)
	}
	want := `{
  "schemaVersion": 1,
  "mode": "diff",
  "base": "main",
  "drifts": [
    {
      "doc": "README.md",
      "section": "Usage",
      "line": 3,
      "files": [
        "a.go",
        "old.go -> new.go"
      ],
      "severity": "high",
      "brokenReferences": [
        {
          "kind": "flag",
          "value": "--gone",
          "line": 5
        },
        {
          "kind": "env-var",
          "value": "APP_TOKEN",
          "line": 6
        }
      ]
    }
  ]
}
`
	if buf.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", buf.String(), want)
	}
}

func TestWriteJSONEmptyListsAreArrays(t *testing.T) {
	for name, drifts := range map[string][]detect.Drift{"nil": nil, "empty": {}} {
		var buf bytes.Buffer
		if err := report.WriteJSON(&buf, report.ModeDiff, "main", drifts); err != nil {
			t.Fatal(err)
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
			t.Fatal(err)
		}
		if string(raw["drifts"]) != "[]" {
			t.Errorf("%s: drifts = %s, want []", name, raw["drifts"])
		}
	}
	var buf bytes.Buffer
	if err := report.WriteJSON(&buf, report.ModeDiff, "main", []detect.Drift{{Doc: "a", Section: "s", Line: 1}}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"files": []`)) {
		t.Errorf("nil files must render as []: %s", buf.String())
	}
}

func TestWriteJSONZeroValueDriftIsLowWithEmptyReferences(t *testing.T) {
	var buf bytes.Buffer
	if err := report.WriteJSON(&buf, report.ModeHistory, "", []detect.Drift{{Doc: "a", Section: "s", Line: 1}}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"\"severity\": \"low\"", "\"brokenReferences\": []"} {
		if !bytes.Contains(buf.Bytes(), []byte(want)) {
			t.Errorf("missing %s in %s", want, buf.String())
		}
	}
}

func TestWriteTextZeroValueSeverityIsLow(t *testing.T) {
	var buf bytes.Buffer
	if err := report.WriteText(&buf, []detect.Drift{{Doc: "a.md", Section: "S", Line: 1, Files: []string{"x.go"}}}); err != nil {
		t.Fatal(err)
	}
	if want := "[low] S\n  a.md:1\n  changed code:\n    x.go\n"; buf.String() != want {
		t.Fatalf("got %q", buf.String())
	}
}
