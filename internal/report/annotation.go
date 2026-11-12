package report

import (
	"fmt"
	"strings"

	"github.com/JtheGunner/doc-drift-checker/internal/config"
	"github.com/JtheGunner/doc-drift-checker/internal/detect"
)

// maxListedFiles caps how many triggering files a one-line message names.
const maxListedFiles = 20

// isHigh reports whether the drift has high severity.
func isHigh(d detect.Drift) bool {
	return d.Severity == config.SeverityHigh
}

// describe builds the one-line message shared by the SARIF and GitHub
// reporters. Every repository-provided value is neutralised and capped.
func describe(d detect.Drift) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Section %q may be outdated: linked code changed but the section did not", clean(d.Section))
	if len(d.Files) > 0 {
		shown := d.Files
		if len(shown) > maxListedFiles {
			shown = shown[:maxListedFiles]
		}
		cleaned := make([]string, len(shown))
		for i, f := range shown {
			cleaned[i] = clean(f)
		}
		b.WriteString(" (changed code: " + strings.Join(cleaned, ", "))
		if extra := len(d.Files) - len(shown); extra > 0 {
			fmt.Fprintf(&b, ", and %d more", extra)
		}
		b.WriteString(")")
	}
	b.WriteString(".")
	if len(d.BrokenReferences) > 0 {
		b.WriteString(" Broken references:")
		for i, r := range d.BrokenReferences {
			if i > 0 {
				b.WriteString(";")
			}
			fmt.Fprintf(&b, " %s %s (line %d)", r.Kind, clean(r.Value), r.Line)
		}
		b.WriteString(".")
	}
	return b.String()
}
