package report

import (
	"encoding/json"
	"io"
	"net/url"
	"strings"

	"github.com/JtheGunner/doc-drift-checker/internal/detect"
)

const (
	sarifSchemaURI = "https://json.schemastore.org/sarif-2.1.0.json"
	sarifVersion   = "2.1.0"
	sarifToolName  = "doc-drift-checker"
	sarifRuleID    = "doc-drift"
	sarifRepoURL   = "https://github.com/JtheGunner/doc-drift-checker"
)

type sarifText struct {
	Text string `json:"text"`
}

type sarifConfig struct {
	Level string `json:"level"`
}

type sarifRule struct {
	ID                   string      `json:"id"`
	ShortDescription     sarifText   `json:"shortDescription"`
	Help                 sarifText   `json:"help"`
	DefaultConfiguration sarifConfig `json:"defaultConfiguration"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	InformationURI string      `json:"informationUri"`
	Rules          []sarifRule `json:"rules"`
}

type sarifArtifact struct {
	URI       string `json:"uri"`
	URIBaseID string `json:"uriBaseId"`
}

type sarifRegion struct {
	StartLine int `json:"startLine"`
}

type sarifPhysical struct {
	ArtifactLocation sarifArtifact `json:"artifactLocation"`
	Region           sarifRegion   `json:"region"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysical `json:"physicalLocation"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	Level     string          `json:"level"`
	Message   sarifText       `json:"message"`
	Locations []sarifLocation `json:"locations"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

// WriteSARIF writes the drifts as a SARIF 2.1.0 log with one run and one
// result per drift, in the order given. A high severity drift is an "error",
// a low one a "warning". The location is the first line of the section in its
// documentation file; the section's end line is not part of the report.
// Text is neutralised like in the other reporters. The tool version is not
// known to the reporters and therefore omitted.
func WriteSARIF(w io.Writer, drifts []detect.Drift) error {
	run := sarifRun{
		Tool: sarifTool{Driver: sarifDriver{
			Name:           sarifToolName,
			InformationURI: sarifRepoURL,
			Rules: []sarifRule{{
				ID:               sarifRuleID,
				ShortDescription: sarifText{Text: "Documentation section may be outdated"},
				Help: sarifText{Text: "Code linked to a documentation section changed while the section's own text did not. " +
					"Review the section and update it, or acknowledge it with doc-drift-checker ack."},
				DefaultConfiguration: sarifConfig{Level: "warning"},
			}},
		}},
		Results: make([]sarifResult, 0, len(drifts)),
	}
	for _, d := range drifts {
		level := "warning"
		if isHigh(d) {
			level = "error"
		}
		run.Results = append(run.Results, sarifResult{
			RuleID:  sarifRuleID,
			Level:   level,
			Message: sarifText{Text: describe(d)},
			Locations: []sarifLocation{{PhysicalLocation: sarifPhysical{
				ArtifactLocation: sarifArtifact{URI: relativeURI(d.Doc), URIBaseID: "%SRCROOT%"},
				Region:           sarifRegion{StartLine: max(d.Line, 1)},
			}}},
		})
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(sarifLog{Schema: sarifSchemaURI, Version: sarifVersion, Runs: []sarifRun{run}})
}

// relativeURI turns a slash-separated repository path into a relative URI
// reference: invalid UTF-8 is replaced and everything unsafe is percent-encoded.
func relativeURI(path string) string {
	path = strings.ToValidUTF8(path, "�")
	return (&url.URL{Path: path}).String()
}
