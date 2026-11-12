# CI integration

doc-drift-checker can run as a pull request check, report drift as annotations or in GitHub code scanning, and run as a local pre-push hook.

| Use | How |
| --- | --- |
| Fail a pull request on drift | [GitHub Action](#github-action) |
| Same without the Action | [Plain workflow](#without-the-action) |
| Show drift in code scanning | [SARIF upload](#sarif-and-code-scanning) |
| Check before pushing | [pre-commit](#pre-commit) |

All examples need the full git history, because `check --base` compares `base...HEAD` and needs the merge base. Use `fetch-depth: 0` in `actions/checkout`; a shallow checkout is rejected.

## Output formats

`check --format` accepts:

- `text` (default) and `json`: see the `internal/report` package comment for the JSON schema.
- `github`: one `::error` (high severity) or `::warning` (low severity) workflow command per drift. GitHub shows them as annotations on the pull request, placed at the first line of the documentation section. Nothing is printed without drift.
- `sarif`: a SARIF 2.1.0 log with one result per drift (level `error` for high, `warning` for low severity), for code scanning.

Exit codes are the same for every format: `0` no drift, `1` drift, `2` usage or runtime error. `--min-severity high` makes only high severity drift fail the run, and `--warn-only` forces exit `0` on drift.

## GitHub Action

The action installs the binary with `go install` and runs `check --base origin/<base> --format <format>` in your repository. The job fails when drift is found (exit `1`) or the check cannot run (exit `2`).

> [!IMPORTANT]
> The `uses:` reference below needs a released tag of this repository. `v0` is a placeholder; replace it with a tag that exists.

```yaml
name: Docs drift
on: pull_request

permissions:
  contents: read

jobs:
  doc-drift:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: JtheGunner/doc-drift-checker@v0
```

Inputs, all optional:

| Input | Default | Meaning |
| --- | --- | --- |
| `base` | pull request base branch, else the repository default branch | Branch to compare against; the check runs on `origin/<base>...HEAD`. |
| `format` | `github` | `text`, `json`, `sarif` or `github`. |
| `min-severity` | empty | `low` or `high`; empty uses `severity.fail-on` from `.docdrift.yaml`. |
| `version` | `latest` | Version to install: a release tag such as `v1.2.3`, a commit hash, or `latest`. |
| `working-directory` | `.` | Repository directory to check. |

The action needs no secrets and only `contents: read`. Inputs are validated before use (`format` against a fixed list, `base` and `version` against a conservative pattern) and are never interpolated into a shell script.

## Without the action

```yaml
name: Docs drift
on: pull_request

permissions:
  contents: read

jobs:
  doc-drift:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v5
        with:
          go-version: stable
      - run: go install github.com/JtheGunner/doc-drift-checker/cmd/doc-drift-checker@latest
      - run: doc-drift-checker check --base "origin/$BASE_REF" --format github
        env:
          BASE_REF: ${{ github.base_ref }}
```

The branch name goes through `env:` instead of being interpolated into the script, which keeps branch names from being executed as shell code.

## SARIF and code scanning

Write the report to a file and upload it with `github/codeql-action/upload-sarif`. Uploading needs `security-events: write`.

`check` exits `1` on drift, which would stop the job before the upload step. Use `--warn-only` for the report step so the upload always runs (runtime errors still exit `2`), and run a second `check` afterwards when the job should also fail on drift.

```yaml
name: Docs drift (code scanning)
on: pull_request

permissions:
  contents: read
  security-events: write

jobs:
  doc-drift:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v5
        with:
          go-version: stable
      - run: go install github.com/JtheGunner/doc-drift-checker/cmd/doc-drift-checker@latest
      - name: Write SARIF report
        run: doc-drift-checker check --base "origin/$BASE_REF" --format sarif --warn-only > drift.sarif
        env:
          BASE_REF: ${{ github.base_ref }}
      - uses: github/codeql-action/upload-sarif@v3
        with:
          sarif_file: drift.sarif
      - name: Fail on drift
        run: doc-drift-checker check --base "origin/$BASE_REF" --format text
        env:
          BASE_REF: ${{ github.base_ref }}
```

Alternatively keep a single step and write `check ... --format sarif > drift.sarif || true`, then upload with `if: always()`. That hides runtime errors, so the two-step form is preferred.

## pre-commit

The repository ships a [pre-commit](https://pre-commit.com) hook, `doc-drift-checker`. pre-commit builds it with Go, so Go must be installed. Replace `<tag>` with a released tag.

```yaml
# .pre-commit-config.yaml
repos:
  - repo: https://github.com/JtheGunner/doc-drift-checker
    rev: <tag>
    hooks:
      - id: doc-drift-checker
```

Install the pre-push stage once per clone: `pre-commit install --hook-type pre-push`.

The hook runs `doc-drift-checker check` on every push (`always_run`, no file names are passed). Without `--base` that is **history mode**: a section drifts when linked code has a newer commit than the section's own lines. It needs no base branch, which suits a local hook, but it is subject to the limits listed in `doc-drift-checker check --help` (squash merges, rewritten history, shallow clones).

To compare against a base branch instead, pass `--base`:

```yaml
    hooks:
      - id: doc-drift-checker
        args: ["--base", "origin/main"]
```
