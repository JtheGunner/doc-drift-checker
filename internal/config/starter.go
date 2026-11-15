package config

import (
	"strconv"
	"strings"
)

// Starter returns the commented starter configuration written by the init
// command. Every active value equals Defaults, so parsing it yields the same
// configuration as having no file; the ignore list is generated from
// DefaultIgnore and cannot drift from it. The mappings example stays commented
// out because it must name real headings and files; so does mode.default,
// because setting it explicitly changes how check runs without --base.
func Starter() []byte {
	var b strings.Builder
	b.WriteString(starterHead)
	for _, glob := range DefaultIgnore() {
		b.WriteString("  - " + strconv.Quote(glob) + "\n")
	}
	b.WriteString(starterTail)
	return []byte(b.String())
}

const starterHead = `# doc-drift-checker configuration.
# Every value below is the built-in default; delete or change what you need.

# Documentation files to check (globs, relative to the repository root).
docs:
  - "README.md"

# Paths whose changes never cause drift. A present ignore key REPLACES the
# built-in list, so repeat every default you still want. "ignore: []" turns
# path ignoring off.
ignore:
`

const starterTail = `
# Explicit section-to-code links, keyed by heading text or anchor ("#usage").
# Values are globs of the code files the section describes. A section with an
# annotation or a mapping ignores its implicit (path-reference) links.
# mappings:
#   Installation:
#     - "cmd/**"
#   "#configuration":
#     - "internal/config/**"

# Detection mode of check and explain when --base is not given. Unset, they run
# history mode. "history" does the same explicitly; "diff" makes a run without
# --base a usage error (pass --base <ref>). --base always selects diff mode.
# mode:
#   default: history

severity:
  # Lowest severity that makes check exit 1: low or high.
  fail-on: low
`
