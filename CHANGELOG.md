# Changelog

All notable changes to this project are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses [Semantic Versioning](https://semver.org/). The release date is filled in when the version tag is created.

## [0.3.0] - Unreleased

### Changed

- The default `ignore` list now includes the release documents `CHANGELOG.md`, `LICENSE`, `LICENSE.md`, `LICENSE.txt` and `CONTRIBUTING.md` (at any depth), so a new changelog entry or a license year no longer reports a README section as outdated. A section that is explicitly linked to one of these files (annotation or `mappings`) no longer reports drift for it either, unless the config sets its own `ignore` list, which still replaces the defaults entirely.

## [0.2.3] - 2026-11-16

### Added

- `links` reports `mappings` keys that match no section (`unmatched mapping:` in text, `unmatchedMappings` in JSON). `check` and `explain` are unchanged.

### Documentation

- The README has a new section, "Choosing good mappings": key forms (full heading text including emoji, or `#` plus the anchor), mapping the file that defines a section, and rolling out with `--warn-only` first. `docs/ci-integration.md` gained a "Getting started" section.

## [0.2.2] - 2026-11-15

### Changed

- The minimal Go version for `go install` and source builds is now 1.25 (it was 1.27.3). The README names the minimum.
- The repository gained a security policy (`SECURITY.md`) and runs its own documentation check on pull requests (warn-only), and its CI now tests on macOS as well as Linux, with the race detector. No change for users of the binary.

### Fixed

- The release workflow no longer runs when the moving major tag (`v0`) is updated, only for version tags such as `v0.2.2`.
- Implicit references to the section's own documentation file or to another file matching the `docs` globs no longer link a section, so they cause no false drift (for example a "Changelog" section that mentions `CHANGELOG.md`). Annotations and `mappings` are unchanged.

## [0.2.1] - 2026-11-15

### Changed

- The neutralisation of control characters in repository-provided text now has one implementation (`internal/textsafe`) shared by the `check`, `links`, `explain` and `init` output instead of three copies. Output is unchanged.
- The test for `init` refusing a FIFO as a documentation file lives in a unix-only test file, so the `internal/cli` tests compile on Windows. No change for users.

## [0.2.0] - 2026-11-15

### Changed

- `check` and `explain` apply `mode.default` when `--base` is not given: `history` runs history mode, `diff` is a usage error asking for `--base`. An unset key behaves as before (history mode). `--base` always selects diff mode.
- `init` lists sections that link more than 10 files as skipped, with the reason and a count in the summary line, instead of omitting them silently. Nothing is written for them.
- `ack` writes `doc-drift.lock` through the same atomic write path as `init`. A new lockfile still gets mode `0644`; an existing one keeps its permission bits.

### Fixed

- The text output of `check` and `links` escapes control characters in repository-provided values (paths, headings, references) and caps their length, as `explain`, SARIF and GitHub output already did.

## [0.1.0] - 2026-11-15

First release.

### Added

- `links` command: shows which code files each documentation section describes, with the origin of every link (annotation, config, implicit), unresolved references and unmapped sections; `--json` for machine-readable output.
- Section-to-code links from `<!-- drift: glob, glob -->` annotations, `mappings` in `.docdrift.yaml`, and implicit file-path and relative-link references.
- `check` command in two modes: diff mode (`--base <ref>`, compares `base...HEAD`) and history mode (no `--base`, compares commit dates, `--ignore-revs-file` for formatting commits).
- Output formats `text`, `json`, `sarif` (SARIF 2.1.0) and `github` (workflow annotations); exit codes `0` no drift, `1` drift, `2` error; `--warn-only`.
- Severity `low` and `high` with broken-reference detection for flags, environment variables and identifiers; `--min-severity` and `severity.fail-on`.
- Relevance filters: ignore globs with sensible defaults, whitespace-only changes, and exported-API analysis for Go sources.
- `ack` command and the `doc-drift.lock` lockfile to confirm that a section is still correct.
- `explain` command that shows why a section is, or is not, reported.
- `init` command that writes a starter `.docdrift.yaml` and suggests (`--write`: inserts) annotations.
- Configuration file `.docdrift.yaml` with `docs`, `ignore`, `mappings`, `mode.default` (reserved) and `severity.fail-on`.
- GitHub Action (`action.yml`) and a pre-commit hook (`.pre-commit-hooks.yaml`); CI guide in `docs/ci-integration.md`.
- Release binaries for macOS and Linux (amd64 and arm64) with checksums, built by GoReleaser when a `v*` tag is pushed.
- MIT license.
