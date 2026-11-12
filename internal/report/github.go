package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/JtheGunner/doc-drift-checker/internal/detect"
)

// maxAnnotationMessage caps the message of one annotation, in characters.
const maxAnnotationMessage = 1000

// annotationTitle is the title shown on every annotation.
const annotationTitle = "doc-drift"

var (
	messageEscaper  = strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A")
	propertyEscaper = strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C")
)

// WriteGitHub writes one GitHub Actions workflow command per drift:
// "::error" for high severity, "::warning" for low, located at the first line
// of the section in its documentation file. The section's end line is not part
// of the report, so endLine is not set. Property values and messages are
// escaped following the workflow command rules and the text is neutralised
// first, so repository content can never start or alter a command. Nothing is
// written when there is no drift.
func WriteGitHub(w io.Writer, drifts []detect.Drift) error {
	for _, d := range drifts {
		command := "warning"
		if isHigh(d) {
			command = "error"
		}
		line := max(d.Line, 1)
		_, err := fmt.Fprintf(w, "::%s file=%s,line=%d,title=%s::%s\n",
			command, propertyEscaper.Replace(cleanMax(d.Doc, 500)), line, annotationTitle,
			messageEscaper.Replace(cleanMax(describe(d), maxAnnotationMessage)))
		if err != nil {
			return err
		}
	}
	return nil
}
