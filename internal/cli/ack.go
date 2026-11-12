package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/JtheGunner/doc-drift-checker/internal/detect"
	"github.com/JtheGunner/doc-drift-checker/internal/gitx"
	"github.com/JtheGunner/doc-drift-checker/internal/links"
	"github.com/JtheGunner/doc-drift-checker/internal/lock"
	"github.com/JtheGunner/doc-drift-checker/internal/relevance"
)

func newAckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ack <section>",
		Short: "Confirm that a section is still correct despite code changes",
		Long: "Confirm that a documentation section still matches its code and record it in " +
			lock.FileName + " in the repository root (commit the file).\n\n" +
			"<section> is the section key <doc path>#<anchor> (the doc path as shown by links, " +
			"the anchor GitHub-style, e.g. README.md#installation), or just the anchor (with or " +
			"without #) when it is unique across all configured docs. Matching is exact.\n\n" +
			"The lockfile stores a sha256 hash of every mapped code file of the section at HEAD " +
			"(paths matching the ignore globs are left out). ack refuses when a mapped file has " +
			"uncommitted changes, when a mapped file is above 4 MiB, and when the section has no " +
			"mapped files. Acknowledging again replaces only that section's entry.\n\n" +
			"check then treats a section with an entry as follows. Diff mode: a drift is " +
			"suppressed while the mapped code (same paths, same hashes) equals the locked state. " +
			"History mode: the entry takes precedence over blame dating; the section drifts iff " +
			"the mapped code differs from the locked state, whatever the dates say, so run ack " +
			"again after updating the documentation to clear a mismatch. Only path ignore rules " +
			"apply to this comparison; whitespace and symbol relevance cannot be evaluated " +
			"against stored hashes, so any content change of a mapped file reopens the section.\n\n" +
			"Exit codes: 0 acknowledged, 2 error (unknown or ambiguous section, nothing to " +
			"acknowledge, uncommitted or oversized mapped file, malformed lockfile).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := runAck(cmd.OutOrStdout(), args[0]); err != nil {
				return &exitError{code: ExitFailure, err: err}
			}
			return nil
		},
	}
}

// runAck records the current state of the section named by arg in the lockfile.
func runAck(w io.Writer, arg string) error {
	in, err := loadRepoInputs()
	if err != nil {
		return err
	}
	locked, err := loadLock(in.root)
	if err != nil {
		return err
	}
	// Run git from the repository root so every path is root-relative. Only
	// files committed at HEAD can be hashed, as in history mode.
	git := gitx.New(in.root)
	headFiles, err := git.HeadFiles()
	if err != nil {
		return err
	}
	in.tracked = headFiles
	docs, err := in.resolveDocs(headFiles)
	if err != nil {
		return err
	}
	key, section, err := findSection(docs, arg)
	if err != nil {
		return err
	}
	state, err := readMappedState(git, relevance.NewFilter(in.cfg.Ignore), section)
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	if len(state.unhashable) > 0 {
		return fmt.Errorf("cannot acknowledge %s: %s too large (above %d bytes) or unreadable at HEAD",
			key, strings.Join(state.unhashable, ", "), detect.MaxFileBytes)
	}
	if len(state.hashes) == 0 {
		return fmt.Errorf("nothing to acknowledge: %s has no mapped code files (see the links command)", key)
	}
	if err := refuseDirty(git, state); err != nil {
		return fmt.Errorf("cannot acknowledge %s: %w", key, err)
	}
	data, err := locked.With(key, state.hashes).Marshal()
	if err != nil {
		return err
	}
	if err := writeLock(in.root, data); err != nil {
		return err
	}
	noun := "files"
	if len(state.hashes) == 1 {
		noun = "file"
	}
	_, err = fmt.Fprintf(w, "acknowledged %s (%d %s)\n", key, len(state.hashes), noun)
	return err
}

// refuseDirty fails when a hashed file differs between HEAD and the working
// tree or index, so the lockfile always describes committed state.
func refuseDirty(git *gitx.Client, state mappedState) error {
	dirty, err := git.DirtyFiles()
	if err != nil {
		return err
	}
	var uncommitted []string
	for _, file := range dirty {
		if _, ok := state.hashes[file]; ok {
			uncommitted = append(uncommitted, file)
		}
	}
	if len(uncommitted) > 0 {
		sort.Strings(uncommitted)
		return fmt.Errorf("uncommitted changes in mapped files: %s (commit them first)", strings.Join(uncommitted, ", "))
	}
	return nil
}

// findSection resolves arg to a section: first as an exact key
// "<doc path>#<anchor>", then as an anchor (with or without "#") that must be
// unique across all documents. Sections without an anchor cannot be keyed.
func findSection(docs []links.DocLinks, arg string) (string, links.SectionLinks, error) {
	anchor := strings.TrimPrefix(arg, "#")
	var byAnchor []string
	found := map[string]links.SectionLinks{}
	for _, doc := range docs {
		for _, section := range doc.Sections {
			if section.Anchor == "" {
				continue
			}
			key := sectionKey(doc.Doc, section.Anchor)
			if key == arg {
				return key, section, nil
			}
			if section.Anchor == anchor {
				byAnchor = append(byAnchor, key)
				found[key] = section
			}
		}
	}
	switch len(byAnchor) {
	case 0:
		return "", links.SectionLinks{}, fmt.Errorf("unknown section %q (use <doc path>#<anchor>, see the links command)", arg)
	case 1:
		return byAnchor[0], found[byAnchor[0]], nil
	}
	sort.Strings(byAnchor)
	return "", links.SectionLinks{}, fmt.Errorf("ambiguous section %q: use one of %s", arg, strings.Join(byAnchor, ", "))
}
