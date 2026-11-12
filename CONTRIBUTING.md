# Contributing to ucat

Thank you for looking. ucat is open source and built in the open: issues, ideas and pull requests are welcome, from a
typo fix to a new feature. Everyone taking part follows the [Code of Conduct](CODE_OF_CONDUCT.md).

## Set up in 15 minutes

You need Go 1.26 or newer, `make` and `git`. A Databricks workspace is **not** needed to build or to run the tests.

```bash
git clone https://github.com/ninilich/ucat.git
cd ucat
make build        # builds ./ucat
make test         # go test ./...
make test-smoke   # bash scripts against the built binary, no Databricks
make lint         # golangci-lint run ./... (golangci-lint v2)
```

If `make test` and `make test-smoke` pass, you are ready. How the code is organised: [docs/contributing/architecture.md](docs/contributing/architecture.md).

## Tests

Three layers.

1. **Unit and integration tests** (`make test`, no Databricks). They use a mock backend (`internal/backend/backendtest`).
   Tests that read `DATABRICKS_*` variables are isolated by `internal/testenv`, so real credentials on your machine do
   not change the result. Golden files live in `testdata/` directories.
2. **Smoke scripts** (`make test-smoke`, no Databricks, no network): bash scripts in `test-scripts/smoke/` build the
   binary and run real commands (`validate`, `init`, flags, messages). See [test-scripts/README.md](test-scripts/README.md).
3. **Live scripts** against a real Unity Catalog sandbox: run by the maintainers before a release and not part of the
   repository. You do not need them to contribute.

The linter is `golangci-lint` v2 with a strict configuration (`.golangci.yml`).

When you add a test:

- A change in the differ, validator or planner: a unit test next to the code. A test must fail when the code is broken:
  check that by breaking the code on purpose.
- A change in a message or a flag: a smoke check or a golden file.
- A new kind of change: also extend the kind test and the JSON golden.

## How to open a pull request

1. Fork the repository on GitHub and clone your fork.
2. Create a branch from `main`: `git switch -c fix-short-description`.
3. Make the change, run the checks from the list below, and commit.
4. Push the branch to your fork and open a pull request to `main` of this repository. GitHub shows the link after
   the push. The pull request template asks what changed and why.
5. The CI runs on your pull request: build, vet and tests on Linux, macOS and Windows, the smoke scripts and the linter.
   It needs no Databricks credentials, so it also runs for forks. Fix what it reports by pushing to the same branch.
6. A maintainer reviews it. Small, focused pull requests are merged faster.

## Before you open a pull request

- Open an issue first for anything bigger than a small fix, so we can agree on the approach.
- Run `go build ./...`, `go vet ./...`, `gofmt -l .`, `make lint`, `go test ./...` and `make test-smoke`. Also
  `go test -race -shuffle=on -count=3 ./...` for changes in concurrent code.
- Add tests. A good test fails when the code it covers is broken: try breaking the code on purpose.
- Do not use `//nolint`. Fix the cause or discuss it in the pull request.
- Golden files (`testdata/*.golden`) are changed only on purpose; explain the diff in the pull request.
- Keep messages, comments and docs in English.
- Compatibility: in 1.x only optional YAML keys and JSON fields are added; the existing ones do not change meaning.
  See [Project layout](docs/project-layout.md#compatibility).

Commit messages: a short prefix and a sentence, for example `fix: ...`, `feat: ...`, `docs: ...`, `test: ...`.

## Where help is welcome

Look for issues labelled `good first issue` and `help wanted`. Ideas for later versions are in [ROADMAP.md](ROADMAP.md).
Tests against a real Unity Catalog are run by the maintainers; if your change needs one, say so in the pull request.

Useful in every size:

- Bug reports with the YAML and the `ucat plan --show-sql` output.
- Real cases that ucat does not handle yet. They decide what comes next, see the [Roadmap](ROADMAP.md).
- Docs: unclear places, missing examples, error messages that Troubleshooting does not cover.
- Tests: a case from your project as a golden file.
- Support for more table properties and column types.

## Reporting a bug

Use the bug report template. Include the output of `ucat version`, the command you ran, the YAML involved (without
secrets) and what you expected. `ucat plan -t <target> --show-sql` shows the exact SQL.

## Security

Do not post credentials or workspace secrets in issues. For a security problem use GitHub's private vulnerability
reporting (the Security tab of the repository) instead of a public issue.

## License

By contributing you agree that your contribution is licensed under the Apache License 2.0, like the rest of the project.
