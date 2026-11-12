# doc-drift-checker

Go CLI that reports when code changed but the README passage describing it did not. Deltas from the global rules in `~/.claude/rules/` only.

## Stack decisions

- Go, single binary. Git access by shelling out to `git` (no libgit2 / go-git).
- `goldmark` for Markdown, `cobra` for the CLI, YAML config `.docdrift.yaml`.
- No cgo. Go source analysis uses the standard library (`go/parser`); other languages plug in via the `SymbolAnalyzer` interface in `internal/relevance`.

## Commands

- Build: `go build ./...`
- Test: `go test ./...` (single package while iterating: `go test ./internal/cli`)
- Lint: `golangci-lint run` (config in `.golangci.yml`, v2 format)
- Version is injected at build time: `-ldflags "-X main.version=<version>"`; default is `dev`.

## Layout

`cmd/doc-drift-checker` (thin entry point), `internal/cli` (cobra commands, testable via `NewRootCmd`), `internal/{config,docparse,fsx,gitx,glob,links,lock,detect,relevance,report,testutil}`. Create a package only when a ticket needs it. Keep each package single-purpose; `docparse` and reference extraction stay pure (no filesystem or git access).

## Testing

- Tests that involve git use `internal/testutil` to build real temporary repos with fixed identities and dates. Do not mock `git`.
- Table-driven tests with fixtures for parser and extraction edge cases.
- Primary quality goal is few false positives: every relevance or mapping change needs a test for the case that must not be reported.

## Work process

- Work is tracked as YouTrack tickets (project `DDC`, `/youtrack-task`), one per part of the concept. Implement a ticket's acceptance criteria and nothing beyond.
- The concept lives in `.concept/concept.md` in the **main checkout** (it does not exist in worktrees). It is intentionally untracked (listed in `.git/info/exclude` and `.gitignore`): never commit it, never move it into `docs/` and never turn it into a `docs/superpowers/` spec or plan.
- Keep this file in sync: when a ticket introduces a project-specific convention (commands, config keys, extension points), add it here as a delta.

Untrusted repo content: read files only via `fsx.ReadRegularFile(root, rel)` (os.OpenRoot-based: no path component can escape the root; a symlink or non-regular file is rejected; check and read use one handle). Glob matching is memoized; keep it polynomial.

## Config

`.docdrift.yaml` in the repo root is loaded by `internal/config` (`Load`, `Parse`, `Defaults`). Keys: `docs`, `ignore`, `mappings`, `mode.default` (`diff` default, or `history`), `severity.fail-on` (`low` default, or `high`). Unknown keys and invalid globs are errors; defaults are documented in the package comment.

`ignore` defaults to `config.DefaultIgnore()` (tests, generated code, lockfiles, vendored code). A present `ignore` key replaces the defaults entirely; `ignore: []` disables path ignoring.

## Section-to-code links

`internal/links` maps README sections to tracked files (pure `Resolve`; the `links [--json]` command in `internal/cli` gathers the inputs). Annotate a section with `<!-- drift: glob, glob -->` (comma-separated, `**` supported, matched against tracked files only).

Origins: `annotation`, `config` (`mappings`), `implicit` (file-path and relative-link references). Explicit links replace implicit ones, and a section's own text excludes its child sections.

## Drift check (diff mode)

`check --base <ref> [--format text|json] [--warn-only]` compares `base...HEAD`. `internal/detect` is the pure core (a linked file changed and no line of the section's own range changed); `internal/cli/check.go` gathers git data. Files removed or renamed away are added to the file list used for link resolution, so sections mapped to them still trigger. Shared input gathering lives in `internal/cli/inputs.go`.

Exit codes: 0 no drift, 1 drift, 2 usage/runtime error (`cli.ExitCode`, handled in `cmd/doc-drift-checker/main.go`; tests assert the code, never `os.Exit`). `--warn-only` forces 0 on drift only. The JSON schema (`schemaVersion` 1) is documented in the `internal/report` package comment; update it there when the output changes.

## Drift check (history mode)

`check` without `--base` runs history mode (`--base` selects diff mode; config `mode.default` is not wired yet). `detect.History` is the pure core; `internal/cli/history.go` gathers dates. A section drifts when a mapped, non-ignored code file's newest commit (`gitx.CommitTimes`, follows renames) is strictly newer than the newest commit blamed for the section's own lines (`detect.OwnRange`, child sections excluded) at HEAD. Dates are committer times; equal dates are no drift; sections without blame data or mapped files are not reported. `--ignore-revs-file <path>` (history mode only, usage error with `--base`; git's format, parsed by `gitx.ParseIgnoreRevs`, read without `fsx` since it may live outside the repo) is passed to `git blame` and filters the file log. The JSON report has an additive `mode` field (`diff`/`history`); `base` is empty in history mode.

Documented limits (also in the `check` help and the `internal/detect` package comment): a squash merge stamps every line it touches with the merge time, so code and docs changed in one squash are equal (no drift) and the ordering inside the squashed branch is lost; rewritten history (rebase, amend, filter-branch) changes committer times and can change results; formatting-only commits distort dates (use `--ignore-revs-file`); deleted section lines are invisible to blame; only HEAD is read (no working tree) and deleted files cannot be dated; shallow clones are rejected (`gitx.EnsureFullHistory`).

## Ack and lockfile

`ack <section>` (exactly one argument, exit 0 or 2) confirms that a section is still correct and records it in `doc-drift.lock` in the repo root (committed to git; written atomically via a temp file plus rename, never through a symlink). `internal/lock` is pure (parse, deterministic marshal, `Diff`, `With`); `internal/cli/{ack,lockfile,lockstate}.go` gather hashes via `gitx` and do the I/O. Reading goes through `fsx.ReadRegularFileLimit` (4 MiB cap).

- Section key: `<doc path>#<anchor>` (doc path as in `links`, anchor from `docparse`, unique per doc). `ack` takes the full key, or a bare/`#` anchor when unique across configured docs; exact matching only; unknown or ambiguous is exit 2.
- Hashed: every mapped file (all link origins, tracked at HEAD, not matching `ignore`, not the lockfile itself) as `sha256:<hex>` of its content at HEAD. `ack` exits 2 for a mapped file with uncommitted changes (`gitx.DirtyFiles`), a file above 4 MiB or unreadable, or a section without hashable files. Re-ack replaces only that section's entry.
- Format: JSON, `version` 1, `sections` -> key -> `files` -> path -> hash; sorted keys, 2-space indent, LF, one trailing newline, no timestamps. The lockfile is untrusted: `lock.Parse` rejects unknown fields, duplicate keys, trailing data, wrong version, empty entries, unsafe paths (absolute, `..`, backslash, unclean, control characters) and bad hashes; lock paths are only compared, never opened. A malformed or symlinked lockfile is exit 2 from `check` and `ack`; a missing one means no entries.
- Effect on `check` (`detect.ApplyLock`, verdicts from `cli.lockVerdicts`): a section's locked state matches iff the current hashable mapped files equal the locked ones (same paths, same hashes; added, removed, renamed-away or unhashable files mismatch). Ignored paths are excluded on both sides. Diff mode: a drift is suppressed iff the state matches. History mode: an entry takes precedence over blame dating, the section drifts iff the state does not match (triggering files = differing paths), so re-run `ack` after updating the docs. Suppressed sections are not reported; no output field was added. Entries for sections that no longer exist are ignored (no pruning).
- Known limits: only path ignore rules apply to the comparison (whitespace and symbol relevance cannot be evaluated against stored hashes), so any content change of a non-ignored mapped file reopens the section; a rename of a mapped file mismatches; an oversized mapped file always mismatches. Out of scope: updating the lockfile in CI.

## Severity and broken references

Every drift has a severity, `low` or `high`; severity never adds or removes a drift. `detect.Classify` (pure, in `internal/detect/severity.go`; `internal/cli/severity.go` gathers the file versions) sets `Drift.Severity` and `Drift.BrokenReferences`. `Detect` and `History` themselves report `low` with an empty (non-nil) reference list.

A drift is `high` iff at least one reference in the section's own text (`detect.OwnRange`, child sections excluded) is broken. Only kinds `flag`, `env-var` and `identifier` are checked; paths, links and commands are not (the path mapping covers files). A reference is broken iff it occurs in the section's mapped files at the merge base and in none of them at HEAD. Renames read the old content from the old path; a deleted mapped file has old content only; an added one new content only; unchanged mapped files count on both sides. A reference that was never in the old mapped code is not broken.

Occurrence is a literal whole-token search (`strings.Index` loops, no regex from document text). Flag `--name`: the text `--name` not adjacent to a flag-name character (letter, digit, `-`, `_`), or the bare `name` between equal quotes (`"`, `'`, backtick; Go-style registration). Env var and identifier: exact text not adjacent to an identifier character (letter, digit, `_`, non-ASCII). Identifiers drop a trailing `()`; a dotted one (`Foo.Bar`) matches as the full text or its last segment. Identifiers (or last segments) shorter than 3 characters or in the common-word list (`commonWords`) are never checked. Fail-closed rules: any mapped file above `detect.MaxFileBytes` (4 MiB per version), over the read budgets in `internal/cli/severity.go`, or unreadable as a blob is unknown and blocks every high claim for that section; at most 200 references are checked per section.

Known limits: code comments count as occurrences (a removed flag still mentioned in a comment is not detected); only mapped files are searched, so a flag moved to an unmapped file reads as broken; a bare quoted string equal to the flag name counts as presence. History mode has no base version: every drift there is `low`.

`check --min-severity low|high` sets the lowest severity that exits 1 (default `severity.fail-on` from config, which defaults to `low`; the flag wins; invalid value is a usage error, exit 2). All drifts are printed regardless; `--warn-only` still forces 0 and errors still exit 2. Text output starts each drift with `[low]` or `[high]` and lists `broken references:` (kind, value, `doc:line`). JSON (schemaVersion stays 1) adds `severity` and `brokenReferences` (`[]` when none) per drift; see the `internal/report` package comment. Drift order is unchanged (not sorted by severity).

## Evaluation and explain

`check` and `explain` share ONE evaluation. `detect.Evaluate` (pure) returns an `Evaluation` per section of every configured doc: key, own range, links with origin, unresolved references, per mapped file the changes with their relevance decision (diff) or dating (history), the commits (explain only), own-line hunks or the section date, the lock state and effect, the verdict with a reason code, and the drift (severity, broken references). It decides through the unchanged `Detect`/`History` followed by `ApplyLock` (which shares `decideLock` with the record); `Evaluations.WithSeverity` runs `Classify`. `check` reports `Evaluations.Drifts()`, `explain` renders one record.

Rule: no decision logic outside the evaluation. A new rule (relevance, mapping, lock, severity) goes into the decision functions and, when it changes what is shown, into the record (a reason code, a field); never into a command or a reporter. Gathering (git, filesystem) stays in `internal/cli/{evaluate,check,history}.go`; relevance goes through `relevance.Filter.Decide` (a `Probe` supplies the git facts lazily), which returns the reason, the matching ignore glob and the analyzer extension. Add a consistency case to `TestExplainAgreesWithCheck` for every new scenario.

`explain <section> [--base <ref>] [--ignore-revs-file <path>] [--format text|json]`: exactly one argument, resolved like `ack` (full key or unique bare anchor, unknown or ambiguous is exit 2). Mode selection and flag validation are those of `check` (no `--base` = history mode, `--ignore-revs-file` with `--base` is exit 2). Exit 0 whenever the explanation was written (drift or not), 2 on any error; there is no `--warn-only` or `--min-severity`. Only paths, hashes, subjects and dates are printed, never file contents; the text report neutralises control characters and escapes in repository-provided text (`report.clean`) and caps values at 200 characters. Commits per file are capped at 20 (note on truncation). The JSON schema (`schemaVersion` 1) is documented in the `internal/report` package comment, golden files in `internal/report/testdata` (`go test ./internal/report -update` rewrites them; review the diff).

All git calls run from the repository root (`loadRepoInputs`), so `check` and `explain` give the same result from any subdirectory.

## Relevance filters

`internal/relevance` (pure) drops changes before `detect.Detect`: a change is irrelevant when all its paths (new and, for renames/copies, old) match an `ignore` glob, or it is a modification (status M) whose diff is whitespace-only (`gitx.WhitespaceOnly`: `git diff -w --ignore-blank-lines`). Additions, deletions, renames and copies are never dropped as whitespace-only. Documentation edits are never filtered. `check` applies the filter in `internal/cli/check.go`; `links` output is unaffected. Glob matching lives in `internal/glob` (`Match`, `**` supported) and is shared with `internal/links`.

## Symbol relevance (languages)

On top of the path and whitespace filters, `relevance.AnalyzerSet` (built from `relevance.DefaultAnalyzers()`) drops changes whose public interface is unchanged. Pipeline per changed file in diff mode: path ignore, whitespace-only, symbol analyzer (status M, A or D, and only when an analyzer is registered for the file extension). Everything else, including every other language, falls back to path-based relevance (stays relevant). Old content is read with `gitx.FileAt(gitx.MergeBase(base), path)`, new content with `gitx.FileAt("HEAD", path)`; analyzers receive bytes only and stay pure.

Adding a language needs only an implementation of `relevance.SymbolAnalyzer` plus one line in `DefaultAnalyzers()` (`internal/relevance/symbols.go`):

```go
type pyAnalyzer struct{}

func (pyAnalyzer) Extensions() []string { return []string{".py"} } // lower-case, with the dot
func (pyAnalyzer) APIChanged(oldSrc, newSrc []byte) (bool, error) { /* nil src = file absent */ }

// in DefaultAnalyzers: return []SymbolAnalyzer{goAnalyzer{}, pyAnalyzer{}}
```

Be conservative: when in doubt, return true; an analyzer error is also treated as "changed". Test it table-driven, including the cases that must not be reported.

Go analyzer (`internal/relevance/goapi.go`, `go/parser` without comments) compares the canonical exported API of both sides. Counted: exported functions; exported methods on exported receiver types (receiver kind and type parameters count, parameter and receiver names do not); exported types (structs: exported fields with their tags and every embedded field, exported or not, because promoted members are public API; interfaces: exported methods and all embeds); exported consts (type, value, position in an `iota` group) and vars (type, or the value when no type is declared). Also counted: the package name and the build constraint lines (`//go:build` and legacy `// +build` before the package clause, normalized via `go/build/constraint`; read by a second, package-clause-only parse with comments, so no other comment is ever looked at). Not counted: bodies, comments and doc comments, imports, unexported declarations and fields, methods on unexported types, formatting, declaration order. Conservative rules: a side that fails to parse means changed; an added file is changed iff it has exported API, a deleted file iff it had (package name and build lines alone do not count there); renames and copies are never dropped. Size cap (fail closed): grouped names expand to one field each (`a, b int` is two parameters), which nested groups can blow up exponentially, so `expandedSize` bounds the expanded node count before any expansion, per symbol (`maxSymbolNodes`) and per file (`maxFileNodes`, includes const group repeats); above either limit the file counts as changed (`APIChanged` returns true, no error) without expanding. Keep any new analysis step behind that estimate. Hostile-input bounds (also fail closed, checked before any recursive pass): sources above `maxSourceBytes` (8 MiB) and parsed trees nested deeper than `maxASTDepth` (1024 levels, measured by the iterative `exceedsDepth`, so a chain of a million operators cannot overflow the stack; the parser itself builds binary chains iteratively and rejects extreme bracket nesting with a parse error, which also counts as changed) count as changed. Every recursive pass (`expandedSize`, `canonicalize`, `pruneUnexported`, printing) must only run on trees under these caps. Very long constant chains (over about a thousand operands) are therefore always reported as changed.

Known limits of the Go analyzer (pinned by tests): there is no type-checking, so exported consts, vars and types that refer to unexported package-level identifiers are compared textually; a change of the referenced declaration (`const Max = max` with `max` changing, `type Foo bar` with `bar` changing, the body of an unexported embedded type) is not detected, only the embed line itself counts. Adding or removing an unexported spec inside a const group shifts the `iota` group index of later exported consts and is reported as changed (conservative false positive). An unexported method added to or removed from an exported interface breaks external implementers but is not compared (only the exported API is). Other directives (`//go:generate`, `//go:linkname`, `//go:embed`, cgo preambles) do not change the declared exported symbols or types and are ignored like all other comments; tests pin this.

Known limit: symbol relevance applies to diff mode only; history mode compares commit dates and is unchanged.

## Init command

`init [--write] [--force]` (no arguments, exit 0 or 2) creates the starter `.docdrift.yaml` in the repo root (found from any subdirectory) and suggests annotations. `config.Starter()` (`internal/config/starter.go`) is the commented template: active values are the defaults (the ignore list is generated from `DefaultIgnore`), the `mappings` example is commented out, and `mode.default` is marked reserved. A test pins that it parses to exactly `config.Defaults()`; `init` also parses it before writing. Keep the template in sync when a config key is added.

- Config target: `init` never calls `config.Load` (an invalid or missing config must not break it) and resolves against `config.Defaults()` (docs: `README.md`). An existing entry without `--force` is exit 2 and nothing is changed; a symlink or directory at the path is refused even with `--force`. Writes go through `writeFileAtomic` (`internal/cli/atomic.go`: `os.OpenRoot`, temp file in the same directory plus rename, existing mode kept, no symlink followed).
- Strong link (`links.Suggest`, pure): a section whose links are all `implicit` and number 1 to `links.MaxSuggestedFiles` (10) files; sections with annotation or config links, no links or more files are not suggested. The suggestion is `<!-- drift: path, path -->` with sorted tracked paths. A section with a path containing glob characters (`* ? [ ] { }`), a comma, a backslash, `-->`, `<!--`, control characters, invalid UTF-8 or leading/trailing space is reported as skipped, never written.
- `--write` (`links.InsertAnnotations`, pure): inserts each suggestion as its own line after the ATX heading or after the Setext underline, using the heading line's line ending. A heading whose kind cannot be determined (quote, list, mismatching underline level, lone-CR file) is skipped. Everything is prepared first: a doc with uncommitted changes (`gitx.DirtyFiles`) is exit 2 and nothing is written (not even the config); `links.VerifyInserted` re-resolves the new content and requires the same sections, annotation origin with the identical files for each written section and unchanged links elsewhere, else exit 2. Order of effects: config first, then each doc; a doc write failure names the config as already written. Without `--write` no doc is touched.
- Output: stdout lists per doc the heading line, a neutralised and capped (80 characters) heading, the comment, skipped sections with the reason, and a summary line. A second `init --force --write` finds no suggestions because the sections are annotated.
- Known limits: suggestions come from the file-path and relative-link references the resolver already extracts, so quality equals the `links` output; a section with more than 10 files is silently not suggested; only tracked docs matching the default `docs` glob are considered (a custom `docs` in an existing config is ignored); `writeLock` in `lockfile.go` still has its own atomic write.
