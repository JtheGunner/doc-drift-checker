<div align="center">

# 🧭 doc-drift-checker

**Find the README passages that went stale when the code changed.**

<code>code changes</code> &nbsp;→&nbsp; <code>linked README section did not</code> &nbsp;→&nbsp; <code>drift report</code>

[![License: MIT](https://img.shields.io/badge/license-MIT-22c55e?style=flat-square)](LICENSE)
[![Go](https://img.shields.io/badge/Go-single%20binary-3776ab?style=flat-square&logo=go&logoColor=white)](#-installation)
[![Platforms](https://img.shields.io/badge/platforms-macOS%20%7C%20Linux-0ea5e9?style=flat-square)](#-installation)
[![CI](https://img.shields.io/badge/CI-GitHub%20Action%20%7C%20pre--commit-8b5cf6?style=flat-square)](docs/ci-integration.md)

</div>

---

## 🎯 What it is

Documentation rots quietly. Someone renames a flag, changes a function signature or moves a file, and the README paragraph that describes it keeps saying the old thing. Nobody notices, because nothing fails.

`doc-drift-checker` links each section of your Markdown documentation to the code files it describes. It then reports every section whose linked code changed while the section's own text did not. It is a single Go binary that reads your git history; it needs no server and no network access.

The main design goal is **few false positives**. Changes that cannot affect documentation (tests, generated code, formatting, unexported Go code) are filtered out before anything is reported.

```text
  README section ──links──▶ code files
        │                       │
   unchanged?              changed?
        └──────── both ─────────┘
                   ▼
             drift report
```

---

## ⚙️ How it works

**Diff mode** (`check --base <ref>`) compares `base...HEAD`. A section drifts when a linked file changed and no line of the section itself changed. This is the mode for pull requests: it needs the merge base, so a full clone (not a shallow one) is required.

**History mode** (`check` without `--base`) needs no base branch, which suits a local check. A section drifts when the newest commit of a linked file is strictly newer than the newest commit that touched the section's own lines (via `git blame`). Dates are committer times.

Sections are linked to code in three ways, see [Linking sections to code](#-linking-sections-to-code). Whatever the mode, you can silence a reviewed section with [`ack`](#ack).

> [!NOTE]
> History mode is date based and has limits: squash merges stamp all touched lines with the merge time, rewritten history changes dates, and formatting-only commits distort them. See [Known limits](#-known-limits).

---

## 📦 Installation

Pick one.

**With Go** (a recent Go toolchain is required):

```sh
go install github.com/JtheGunner/doc-drift-checker/cmd/doc-drift-checker@latest
```

**From a release:** download the archive for your platform (macOS or Linux, amd64 or arm64) from the [GitHub Releases](https://github.com/JtheGunner/doc-drift-checker/releases) page, verify it against the published `checksums.txt`, unpack it and put `doc-drift-checker` on your `PATH`. Releases appear once a version tag has been published.

**From source:**

```sh
git clone https://github.com/JtheGunner/doc-drift-checker.git
cd doc-drift-checker
go build -o doc-drift-checker ./cmd/doc-drift-checker
```

`doc-drift-checker --version` prints the installed version (`dev` for a plain source build).

`git` must be installed and available on the `PATH`; the tool shells out to it.

> [!NOTE]
> A Homebrew tap is planned but does not exist yet. Until it does, use `go install` or the release archives.

---

## 🚀 Quick start

Run these inside a git repository that has a `README.md`:

```sh
doc-drift-checker init            # write a starter .docdrift.yaml and suggest annotations
doc-drift-checker links           # see which code each section is linked to
doc-drift-checker check --base origin/main
```

Example `links` output, for a README whose "Usage" section mentions `cmd/main.go`:

```text
README.md
  Demo (lines 1-9)
    unmapped
  Usage (lines 3-5)
    cmd/main.go [implicit]
  License (lines 7-9)
    unmapped
```

After a branch changed the exported API of `cmd/main.go` and renamed the `--verbose` flag that the section mentions, `check` reports:

```text
[high] Usage
  README.md:3
  changed code:
    cmd/main.go
  broken references:
    flag --verbose (README.md:5)
```

and exits with code `1`.

---

## 🧰 Commands

| | Command | Purpose |
| --- | --- | --- |
| 🔗 | [`links`](#links) | Show which code files each section describes |
| 🔍 | [`check`](#check) | Report sections whose code changed while the section did not |
| ✅ | [`ack`](#ack) | Confirm that a section is still correct |
| 💬 | [`explain`](#explain) | Explain why a section is, or is not, reported |
| ✨ | [`init`](#init) | Create a starter config and suggest annotations |

Global flags: `-h, --help` and `-v, --version`. Run `doc-drift-checker <command> --help` for the full text of each command. The commands operate on the git repository that contains the current directory and give the same result from any subdirectory.

### `links`

```sh
doc-drift-checker links [--json]
```

Prints every section of the configured documentation files with its linked code files and the origin of each link (`annotation`, `config`, `implicit`), plus references that match no tracked file and sections without links. `--json` prints the mapping as JSON. Exit code `0` on success, `2` on error.

### `check`

```sh
doc-drift-checker check [--base <ref>] [--format text|json|sarif|github] \
  [--min-severity low|high] [--warn-only] [--ignore-revs-file <path>]
```

| Flag | Meaning |
| --- | --- |
| `--base <ref>` | Diff mode: compare `<ref>...HEAD`. Without it, history mode runs. |
| `--format` | `text` (default), `json`, `sarif` (SARIF 2.1.0 for code scanning) or `github` (workflow annotations). |
| `--min-severity` | Lowest severity that exits `1`: `low` or `high`. Default: `severity.fail-on` from the config, else `low`. All drifts are printed regardless. |
| `--warn-only` | Exit `0` even when drift is found. Errors still exit `2`. |
| `--ignore-revs-file <path>` | History mode only: file of commit hashes to skip when dating, in git's `blame.ignoreRevsFile` format. Combined with `--base` it is a usage error. |

Exit codes: `0` no drift (or none at or above the minimum severity), `1` drift, `2` usage or runtime error. They are identical for every format.

```sh
# pull request check against the target branch
doc-drift-checker check --base origin/main

# local check without a base branch, ignoring a formatting commit
doc-drift-checker check --ignore-revs-file .git-blame-ignore-revs

# only fail on drift with broken references, annotate a pull request
doc-drift-checker check --base origin/main --min-severity high --format github
```

### `ack`

```sh
doc-drift-checker ack <section>
```

Confirms that a section still matches its code and records it in [`doc-drift.lock`](#-lockfile-and-ack). `<section>` is `<doc path>#<anchor>` (for example `README.md#installation`) or just the anchor when it is unique across all configured documents. Matching is exact. Exit code `0` on success, `2` on error (unknown or ambiguous section, a mapped file with uncommitted changes, a mapped file above 4 MiB, a section without mapped files, a malformed lockfile).

```sh
doc-drift-checker ack usage
```

```text
acknowledged README.md#usage (1 file)
```

### `explain`

```sh
doc-drift-checker explain <section> [--base <ref>] [--ignore-revs-file <path>] [--format text|json]
```

Shows how `check` arrives at its result for one section: the resolved links, every mapped file with the rule that kept or dropped it, the commits touching it, whether the section's own lines changed, the lockfile state and the verdict with its reason. It uses the same evaluation as `check`, so the answer always agrees. Mode selection works like `check`. Only paths, hashes, commit subjects and dates are printed, never file contents. Exit code `0` whenever the explanation was written (drift or not), `2` on error.

```sh
doc-drift-checker explain usage --base origin/main
```

```text
Section:  Usage
Key:      README.md#usage
Mode:     diff (base main)

Verdict:  no drift
Reason:   all changes to mapped files are dropped by the relevance rules
...
Files:
  cmd/main.go (implicit)
    status: modified
    relevance: dropped, exported API unchanged (analyzer .go)
    triggers drift: no
```

### `init`

```sh
doc-drift-checker init [--write] [--force]
```

Writes a starter `.docdrift.yaml` (all values are the built-in defaults, examples are commented out) and suggests `<!-- drift: ... -->` annotations for sections whose implicit links are strong: all links are implicit and the section links 1 to 10 tracked files. Suggestions are only printed unless `--write` is given; `--write` inserts each one as its own line directly after the section's heading and refuses a documentation file with uncommitted changes. An existing `.docdrift.yaml` is not replaced unless `--force` is given. Exit code `0` on success, `2` on error.

```text
wrote .docdrift.yaml
README.md
  line 3  Usage
    <!-- drift: cmd/main.go -->
1 annotation suggestion(s), not written (run init --write to insert them)
```

---

## 🔗 Linking sections to code

Each Markdown heading starts a section. A section's own text does not include its child sections. Links are resolved against **tracked files only**.

**Annotation.** Put an HTML comment in the section, for example right below the heading. Globs are comma separated and `**` matches across directories:

```markdown
## Configuration

<!-- drift: internal/config/**, cmd/app/flags.go -->
```

**Config mapping.** The same link can live in `.docdrift.yaml`, keyed by heading text or anchor:

```yaml
mappings:
  Configuration:
    - "internal/config/**"
  "#installation":
    - "cmd/**"
```

**Implicit.** Without an annotation or mapping, file paths in the section text (such as `` `cmd/main.go` ``) and relative Markdown links to tracked files link the section to those files.

Explicit links (annotation or mapping) **replace** implicit ones for that section. Run `doc-drift-checker links` to see the result and where each link comes from.

---

## 🛠️ Configuration

The optional `.docdrift.yaml` in the repository root configures the tool. Unknown keys and invalid globs are errors (exit `2`). `doc-drift-checker init` writes a commented starter file.

```yaml
# Documentation files to check (globs, relative to the repository root).
docs:
  - "README.md"
  - "docs/**/*.md"

# Paths whose changes never cause drift.
ignore:
  - "**/*_test.go"
  - "**/testdata/**"
  - "**/vendor/**"

# Explicit section-to-code links, keyed by heading text or anchor.
mappings:
  Installation:
    - "cmd/**"

mode:
  default: diff        # reserved, see below

severity:
  fail-on: low         # low (default) or high
```

| Key | Default | Meaning |
| --- | --- | --- |
| `docs` | `["README.md"]` | Documentation files to check. |
| `ignore` | tests, generated code, lockfiles and vendored code | Globs of paths whose changes never cause drift. |
| `mappings` | none | Section-to-code links by heading text or anchor. |
| `mode.default` | `diff` | `diff` or `history`. **Reserved:** `check` does not apply it yet. It selects history mode without `--base` and diff mode with `--base`. |
| `severity.fail-on` | `low` | Lowest severity that makes `check` exit `1`. The `--min-severity` flag wins. |

> [!IMPORTANT]
> A present `ignore` key **replaces** the built-in list entirely; repeat every default you still want. `ignore: []` turns path ignoring off. The built-in list is what `init` writes into the starter file.

---

## 🚦 Severity and broken references

Every drift is `low` or `high`. Severity never adds or removes a drift; it only controls which drifts fail the run (`--min-severity`, `severity.fail-on`).

A drift is **high** when the section's own text mentions a flag (`--name`), an environment variable or an identifier that existed in the section's linked code at the merge base and exists in none of it now. Such a reference is listed as a broken reference with its line in the document. Paths, links and commands are not checked, since the link itself already covers files. Identifiers shorter than three characters and common words are never checked. In history mode there is no base version, so every drift is `low`.

---

## 🧹 Relevance filters

To keep the report quiet, a change is dropped before drift is decided when:

1. **Path:** every path of the change matches an `ignore` glob (tests, generated code, lockfiles and vendored code by default).
2. **Whitespace:** a modified file differs only in whitespace or blank lines.
3. **Go API (diff mode):** a changed `.go` file has the same exported API on both sides. Function bodies, comments, imports and unexported code do not count; exported functions, methods, types, struct fields, constants and variables, the package name and build constraints do. A file that fails to parse counts as changed.

Additions, deletions and renames are never dropped as whitespace-only, and edits to documentation are never filtered. Files in other languages fall back to path-based relevance: any change not ignored by path or whitespace counts.

---

## ✅ Lockfile and ack

Sometimes the code changed but the section is still right. Run `doc-drift-checker ack <section>` and commit the resulting `doc-drift.lock`. It stores a `sha256` hash of every mapped code file of the section:

```json
{
  "version": 1,
  "sections": {
    "README.md#usage": {
      "files": {
        "cmd/main.go": "sha256:0b18…"
      }
    }
  }
}
```

In **diff mode** the drift of an acknowledged section is suppressed while its mapped files equal the locked state. In **history mode** the entry takes precedence over dates: the section drifts exactly when the mapped files differ from the locked state, so run `ack` again after updating the documentation. Only path ignore rules apply to this comparison, so any content change of a non-ignored mapped file (including a rename) reopens the section. Entries of sections that no longer exist are ignored.

---

## 🤖 CI integration

doc-drift-checker ships a composite GitHub Action, SARIF output for code scanning and a pre-commit hook. A minimal pull request check:

```yaml
jobs:
  doc-drift:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: JtheGunner/doc-drift-checker@v0
```

> [!IMPORTANT]
> The Action is referenced by tag and therefore needs a released tag of this repository; replace `v0` with a tag that exists. The checkout must have full history (`fetch-depth: 0`).

Details, inputs, the SARIF upload and the pre-commit hook are in [docs/ci-integration.md](docs/ci-integration.md).

---

## ⚠️ Known limits

- **History mode** relies on commit dates. A squash merge stamps every touched line with the merge time, so code and docs changed in one squash look equal and are not reported. Rewritten history (rebase, amend) changes dates. Formatting-only commits distort dates; list them with `--ignore-revs-file`. Deleted section lines are invisible, only `HEAD` is read (not the working tree), deleted files cannot be dated, and shallow clones are rejected (`git fetch --unshallow`).
- **Symbol analysis** exists for Go only, and only in diff mode. It does not type-check: a change of an unexported declaration that an exported constant, variable or type refers to is not detected. Very long constant chains and files above 8 MiB are always reported as changed.
- **Broken references** are matched literally on whole tokens. A removed flag that is still mentioned in a code comment is not detected, a flag moved to a file the section is not linked to reads as broken, and mapped files above 4 MiB are never claimed to be broken.
- **Lockfile** comparison cannot apply whitespace or symbol relevance; the mapped files must match exactly.
- **Config:** `mode.default` is reserved and not applied yet.
- `init` suggests annotations only for sections with 1 to 10 implicitly linked files and only for the default `docs` glob (`README.md`).

---

## 🤝 Contributing

Issues and pull requests are welcome. Please open an issue first for larger changes.

```sh
go build ./...
go test ./...
golangci-lint run
```

Tests that involve git build real temporary repositories; `git` must be installed. Every change to relevance or link resolution needs a test for the case that must **not** be reported.

Releases are built by [GoReleaser](https://goreleaser.com) when a `v*` tag is pushed, see [`.goreleaser.yaml`](.goreleaser.yaml).

---

## 📄 License

MIT, see [LICENSE](LICENSE).

## 📝 Changelog

See [CHANGELOG.md](CHANGELOG.md).

---

<div align="center">
<sub>Keep your docs as current as your code.</sub>
</div>
