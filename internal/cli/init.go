package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/JtheGunner/doc-drift-checker/internal/config"
	"github.com/JtheGunner/doc-drift-checker/internal/docparse"
	"github.com/JtheGunner/doc-drift-checker/internal/fsx"
	"github.com/JtheGunner/doc-drift-checker/internal/gitx"
	"github.com/JtheGunner/doc-drift-checker/internal/links"
)

// maxPrintedRunes caps repository-provided text (headings) in the output.
const maxPrintedRunes = 80

func newInitCmd() *cobra.Command {
	var write, force bool
	cmd := &cobra.Command{
		Use:   "init [--write] [--force]",
		Short: "Create a starter " + config.FileName + " and suggest section annotations",
		Long: "Create a starter " + config.FileName + " in the repository root and suggest " +
			"annotations for documentation sections with strong implicit links.\n\n" +
			"The starter config lists the built-in defaults as active values and shows " +
			"examples as comments. An existing " + config.FileName + " is never overwritten " +
			"unless --force is given; a symlink or directory at that path is always refused. " +
			"The file is replaced atomically.\n\n" +
			"A section is strongly linked when all its links are implicit (file paths and " +
			"relative links in its text) and it links 1 to " + fmt.Sprint(links.MaxSuggestedFiles) +
			" tracked files. The suggestion is the annotation \"<!-- drift: path, path -->\" " +
			"listing those files. Sections with explicit links, no links or more files are not " +
			"suggested, and neither are sections with file names that cannot be written safely " +
			"(glob characters, commas, comment markers, control characters).\n\n" +
			"Suggestions are only printed. With --write each one is inserted as its own line " +
			"directly after its heading in the documentation file (after the underline of an " +
			"underlined heading). --write refuses a documentation file with uncommitted changes, " +
			"so the annotations form a clean diff, and writes nothing when the result does not " +
			"resolve as suggested. Suggestions are resolved against the built-in defaults " +
			"(docs: README.md), not against an existing config.\n\n" +
			"Exit codes: 0 done, 2 error (existing config without --force, unsafe target, " +
			"uncommitted documentation file with --write, not a git repository).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := runInit(cmd.OutOrStdout(), write, force); err != nil {
				return &exitError{code: ExitFailure, err: err}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&write, "write", false, "insert the suggested annotations into the documentation files")
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing "+config.FileName)
	return cmd
}

// docPlan is the init result for one documentation file.
type docPlan struct {
	doc         string
	src         []byte
	before      links.DocLinks
	suggestions []links.Suggestion
	skipped     []links.Skipped
	// written is the new content (nil when nothing is written) and applied the
	// suggestions it contains.
	written []byte
	applied []links.Suggestion
}

func runInit(w io.Writer, write, force bool) error {
	data := config.Starter()
	if _, err := config.Parse(data, config.FileName); err != nil {
		return fmt.Errorf("internal error: starter config is invalid: %w", err)
	}
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	root, err := gitx.New(wd).Root()
	if err != nil {
		return fmt.Errorf("find repository root: %w", err)
	}
	git := gitx.New(root)
	if err := checkConfigTarget(root, force); err != nil {
		return err
	}
	plans, err := planDocs(git, root)
	if err != nil {
		return err
	}
	if write {
		if err := prepareWrites(git, plans); err != nil {
			return err
		}
	}
	if err := writeFileAtomic(root, config.FileName, data, 0o644); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "wrote %s\n", config.FileName); err != nil {
		return err
	}
	var written []string
	for _, p := range plans {
		if p.written == nil {
			continue
		}
		if err := writeFileAtomic(root, p.doc, p.written, 0o644); err != nil {
			return fmt.Errorf("%s was written, but annotating %s failed (%s written so far): %w",
				config.FileName, p.doc, strings.Join(written, ", "), err)
		}
		written = append(written, p.doc)
	}
	return printPlans(w, plans, write)
}

// checkConfigTarget refuses to touch an existing config entry without force,
// and a non-regular one even with force.
func checkConfigTarget(root string, force bool) error {
	target := filepath.Join(root, config.FileName)
	info, err := os.Lstat(target)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat %s: %w", config.FileName, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s exists and is not a regular file (symlinks are not followed); remove it first", config.FileName)
	}
	if !force {
		return fmt.Errorf("%s already exists; use --force to replace it", config.FileName)
	}
	return nil
}

// planDocs reads every documentation file selected by the default config and
// computes its annotation suggestions. The repository's own config is not read:
// init must work next to a missing or broken one.
func planDocs(git *gitx.Client, root string) ([]*docPlan, error) {
	tracked, err := git.TrackedFiles()
	if err != nil {
		return nil, err
	}
	var plans []*docPlan
	for _, doc := range links.MatchDocs(config.Defaults().Docs, tracked) {
		src, err := fsx.ReadRegularFile(root, filepath.FromSlash(doc))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", doc, err)
		}
		before := links.Resolve(doc, docparse.ParseSections(src), config.Defaults(), tracked)
		suggestions, skipped := links.Suggest(before)
		plans = append(plans, &docPlan{doc: doc, src: src, before: before, suggestions: suggestions, skipped: skipped})
	}
	return plans, nil
}

// prepareWrites computes and verifies the new content of every document with
// suggestions, before anything is written. A document with uncommitted
// changes, or one whose annotated version does not resolve as suggested, fails
// the whole run.
func prepareWrites(git *gitx.Client, plans []*docPlan) error {
	var dirty map[string]bool
	tracked, err := git.TrackedFiles()
	if err != nil {
		return err
	}
	for _, p := range plans {
		if len(p.suggestions) == 0 {
			continue
		}
		if dirty == nil {
			files, err := git.DirtyFiles()
			if err != nil {
				return err
			}
			dirty = map[string]bool{}
			for _, f := range files {
				dirty[f] = true
			}
		}
		if dirty[p.doc] {
			return fmt.Errorf("cannot annotate %s: it has uncommitted changes (commit them first, nothing was written)", p.doc)
		}
		out, applied, skipped := links.InsertAnnotations(p.src, p.suggestions)
		p.skipped = append(p.skipped, skipped...)
		if len(applied) == 0 {
			continue
		}
		after := links.Resolve(p.doc, docparse.ParseSections(out), config.Defaults(), tracked)
		if err := links.VerifyInserted(p.before, after, applied); err != nil {
			return fmt.Errorf("refusing to annotate: %w (nothing was written)", err)
		}
		p.written, p.applied = out, applied
	}
	return nil
}

// printPlans reports the suggestions per document. With write set, the ones in
// applied are reported as written.
func printPlans(w io.Writer, plans []*docPlan, write bool) error {
	suggested, written := 0, 0
	for _, p := range plans {
		if len(p.suggestions) == 0 && len(p.skipped) == 0 {
			continue
		}
		if _, err := fmt.Fprintln(w, clean(p.doc)); err != nil {
			return err
		}
		for _, s := range p.suggestions {
			if _, err := fmt.Fprintf(w, "  line %d  %s\n    %s\n", s.Line, clean(s.Heading), s.Comment()); err != nil {
				return err
			}
		}
		for _, s := range p.skipped {
			if _, err := fmt.Fprintf(w, "  line %d  %s\n    skipped: %s\n", s.Line, clean(s.Heading), clean(s.Reason)); err != nil {
				return err
			}
		}
		suggested += len(p.suggestions)
		written += len(p.applied)
	}
	switch {
	case suggested == 0:
		_, err := fmt.Fprintln(w, "no annotation suggestions")
		return err
	case !write:
		_, err := fmt.Fprintf(w, "%d annotation suggestion(s), not written (run init --write to insert them)\n", suggested)
		return err
	}
	_, err := fmt.Fprintf(w, "%d annotation suggestion(s), %d written\n", suggested, written)
	return err
}

// clean makes repository-provided text safe to print: control and other
// non-printable characters become "?", and long text is cut.
func clean(s string) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n == maxPrintedRunes {
			b.WriteString("...")
			break
		}
		if unicode.IsControl(r) || !unicode.IsPrint(r) && r != ' ' {
			r = '?'
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}
