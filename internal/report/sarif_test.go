package report_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/JtheGunner/doc-drift-checker/internal/config"
	"github.com/JtheGunner/doc-drift-checker/internal/detect"
	"github.com/JtheGunner/doc-drift-checker/internal/docparse"
	"github.com/JtheGunner/doc-drift-checker/internal/report"
)

// The schema in testdata/sarif-schema-2.1.0.json is the unmodified official
// SARIF 2.1.0 JSON schema from
// https://raw.githubusercontent.com/oasis-tcs/sarif-spec/main/sarif-2.1/schema/sarif-schema-2.1.0.json
// (last upstream commit touching the file: a560296ca8c921f3bdb8d4a8db57ab83dae968a7,
// sha256 c3b4bb2d6093897483348925aaa73af03b3e3f4bd4ca38cef26dcb4212a2682e).
// It is compiled from memory, so the test never touches the network.
func sarifSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "sarif-schema-2.1.0.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	doc, err := jsonschema.UnmarshalJSON(f)
	if err != nil {
		t.Fatal(err)
	}
	const url = "https://example.invalid/sarif-schema-2.1.0.json"
	c := jsonschema.NewCompiler()
	if err := c.AddResource(url, doc); err != nil {
		t.Fatal(err)
	}
	schema, err := c.Compile(url)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func validateSARIF(t *testing.T, schema *jsonschema.Schema, data []byte) {
	t.Helper()
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, data)
	}
	if err := schema.Validate(inst); err != nil {
		t.Fatalf("SARIF does not match the schema: %v\n%s", err, data)
	}
}

func sarifOf(t *testing.T, drifts []detect.Drift) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := report.WriteSARIF(&buf, drifts); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

var hostileDrifts = []detect.Drift{
	{
		Doc: "docs/we ird\n::error::x%,:.md", Section: "Head\n::set-output name=a::b\r\x1b[31m\x00 \xff" + strings.Repeat("x", 5000),
		Line: 4, Files: []string{"a\nb.go", "bad\xffname.go", strings.Repeat("f", 500)},
		Severity: config.SeverityHigh,
		BrokenReferences: []docparse.Reference{
			{Kind: docparse.KindFlag, Value: "--g\none ", Line: 5},
		},
	},
}

func TestWriteSARIFMatchesSchema(t *testing.T) {
	schema := sarifSchema(t)
	cases := map[string][]detect.Drift{
		"no drift":   nil,
		"low and hi": sample,
		"hostile":    hostileDrifts,
		"zero value": {{Doc: "a.md", Section: "S", Line: 1}},
		"no files":   {{Doc: "a.md", Section: "S", Line: 1, Severity: config.SeverityLow}},
	}
	for name, drifts := range cases {
		t.Run(name, func(t *testing.T) {
			validateSARIF(t, schema, sarifOf(t, drifts))
		})
	}
}

func TestSARIFSchemaRejectsInvalidDocument(t *testing.T) {
	schema := sarifSchema(t)
	for name, doc := range map[string]string{
		"wrong version": `{"version": "1.0", "runs": []}`,
		"no runs":       `{"version": "2.1.0"}`,
		"bad level":     `{"version": "2.1.0", "runs": [{"tool": {"driver": {"name": "x"}}, "results": [{"level": "fatal", "message": {"text": "m"}}]}]}`,
		"no message":    `{"version": "2.1.0", "runs": [{"tool": {"driver": {"name": "x"}}, "results": [{"level": "error"}]}]}`,
	} {
		inst, err := jsonschema.UnmarshalJSON(strings.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		if schema.Validate(inst) == nil {
			t.Errorf("%s: invalid document was accepted, the validation is vacuous", name)
		}
	}
}

func TestWriteSARIFGolden(t *testing.T) {
	golden(t, "sarif_drifts.golden", string(sarifOf(t, sample)))
	golden(t, "sarif_empty.golden", string(sarifOf(t, nil)))
}

func TestWriteSARIFContent(t *testing.T) {
	var got struct {
		Version string `json:"version"`
		Runs    []struct {
			Tool struct {
				Driver struct {
					Name  string           `json:"name"`
					Rules []map[string]any `json:"rules"`
				} `json:"driver"`
			} `json:"tool"`
			Results []struct {
				RuleID    string `json:"ruleId"`
				Level     string `json:"level"`
				Message   struct{ Text string }
				Locations []struct {
					PhysicalLocation struct {
						ArtifactLocation struct {
							URI       string `json:"uri"`
							URIBaseID string `json:"uriBaseId"`
						} `json:"artifactLocation"`
						Region struct {
							StartLine int `json:"startLine"`
						} `json:"region"`
					} `json:"physicalLocation"`
				} `json:"locations"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(sarifOf(t, sample), &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != "2.1.0" || len(got.Runs) != 1 {
		t.Fatalf("unexpected header: %+v", got)
	}
	run := got.Runs[0]
	if run.Tool.Driver.Name != "doc-drift-checker" || len(run.Tool.Driver.Rules) != 1 {
		t.Fatalf("unexpected driver: %+v", run.Tool.Driver)
	}
	if len(run.Results) != 2 {
		t.Fatalf("results = %d, want 2", len(run.Results))
	}
	if r := run.Results[0]; r.RuleID != "doc-drift" || r.Level != "error" {
		t.Errorf("high drift: %+v", r)
	}
	if r := run.Results[1]; r.Level != "warning" {
		t.Errorf("low drift level = %q", r.Level)
	}
	loc := run.Results[1].Locations[0].PhysicalLocation
	if loc.ArtifactLocation.URI != "docs/x.md" || loc.ArtifactLocation.URIBaseID != "%SRCROOT%" || loc.Region.StartLine != 9 {
		t.Errorf("location = %+v", loc)
	}
}

func TestWriteSARIFEmptyResultsIsArray(t *testing.T) {
	if !bytes.Contains(sarifOf(t, nil), []byte(`"results": []`)) {
		t.Fatal("no drift must give an empty results array")
	}
}

func TestWriteSARIFHostileTextHasNoRawControlCharacters(t *testing.T) {
	out := sarifOf(t, hostileDrifts)
	if strings.Count(string(out), "\r") != 0 || bytes.Contains(out, []byte{0x1b}) || bytes.Contains(out, []byte{0}) {
		t.Fatalf("raw control characters in output: %q", out)
	}
	if strings.Contains(string(out), strings.Repeat("x", 300)) {
		t.Fatal("long heading was not cut")
	}
}
