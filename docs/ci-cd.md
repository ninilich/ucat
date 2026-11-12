# CI/CD

ucat is built for CI/CD: the plan is a reviewable step, `apply` is non-interactive with `--auto-approve`, and the
exit codes and `--json` are stable. Environments are separate runs: there is no "promote" command.

## Credentials in CI

Use a service principal with OAuth machine-to-machine credentials in environment variables (`DATABRICKS_HOST`,
`DATABRICKS_CLIENT_ID`, `DATABRICKS_CLIENT_SECRET`), or OIDC federation. For several parallel jobs do not use a
Databricks CLI profile with secure token storage: see [Troubleshooting](troubleshooting.md#cannot-configure-default-credentials).
The service principal needs the permissions listed in [Authentication](authentication.md#minimum-permissions).

## Exit codes in pipelines

| Command | 0 | 1 | 2 | 130 |
|---|---|---|---|---|
| `plan` | ok | error | | interrupted |
| `plan --detailed-exitcode` | no changes | error | changes present | interrupted |
| `apply` | ok | error, or the prompt was declined | | interrupted |
| `validate` | ok | a check failed | | |

`--detailed-exitcode` is the gate: `2` means "there are changes". Run `plan` on pull requests and `apply` on merge. A
cancelled job exits with 130; run `apply` again to continue, see [Commands](commands.md).

## GitHub Actions example

```yaml
name: tables
on:
  pull_request:
    paths: ["tables/**", "catalogs/**", "targets/**", "ucat.yml"]
  push:
    branches: [main]
    paths: ["tables/**", "catalogs/**", "targets/**", "ucat.yml"]

env:
  DATABRICKS_HOST: ${{ vars.DATABRICKS_HOST }}

jobs:
  plan:
    if: github.event_name == 'pull_request'
    runs-on: ubuntu-latest
    environment: prod-plan   # a read-only service principal, see "Running plan on pull requests"
    env:
      DATABRICKS_CLIENT_ID: ${{ secrets.DATABRICKS_CLIENT_ID }}
      DATABRICKS_CLIENT_SECRET: ${{ secrets.DATABRICKS_CLIENT_SECRET }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: "1.26" }
      - run: go install github.com/ninilich/ucat@v1.0.0
      - run: ucat validate
      - run: ucat plan -t prod --show-sql

  apply:
    if: github.event_name == 'push'
    runs-on: ubuntu-latest
    environment: prod
    concurrency: { group: ucat-prod, cancel-in-progress: false }   # one apply at a time per target
    env:
      DATABRICKS_CLIENT_ID: ${{ secrets.DATABRICKS_CLIENT_ID }}
      DATABRICKS_CLIENT_SECRET: ${{ secrets.DATABRICKS_CLIENT_SECRET }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: "1.26" }
      - run: go install github.com/ninilich/ucat@v1.0.0
      - run: ucat apply -t prod --auto-approve
```

The example pins ucat to `v1.0.0`; adjust the version, the names and the paths to your project. The `prod` environment
can hold an approval rule, a manual gate before `apply`. `plan` and `apply` use different environments, each with its
own secrets (the same names, different values), so `plan` runs as a read-only service principal: see the next section.

## Running plan on pull requests

The example runs `plan` for a pull request. `plan` runs no SQL and needs no warehouse, but it still reads your real
Unity Catalog, so treat its credentials with care:

- Give `plan` its own service principal with **read-only** access to the schemas: no `MODIFY`, no `CREATE TABLE`.
  Keep the `apply` principal in a different environment (`prod`), with its approval rule.
- Do not run it on pull requests from forks, and do not work around the missing secrets (for example with
  `pull_request_target`). A pull request can also change the workflow file, so the credentials of the `prod-plan`
  environment must not be able to change anything.

## Plan and apply are separate runs

There is no saved plan. `ucat apply` reads the live state again and computes a new plan at that moment, then applies it.
What you reviewed on the pull request is the plan of that day; what `apply` runs is the plan of the merge. They differ
if the live state or the files changed in between: a table was edited by hand, or another pull request was merged first.

To stop an unexpected change, run `ucat plan -t prod --detailed-exitcode` right before `apply` and compare its output
with what was approved (exit code `2` means there are changes).

## Concurrent runs

ucat does not lock anything: two `apply` runs against one target at the same time can fail or apply the changes in an
order nobody reviewed. Make your CI run one `apply` at a time per target, as `concurrency` does in the example. Runs against different targets
are independent.

## Deleting tables from a pipeline

Two ways, both explicit:

- Set `drop: true` in the table file and merge; `apply --auto-approve` drops it. Delete the file in a later commit.
- Run `ucat drop -t prod --schema <s> --table <t> --auto-approve` for a table that has no YAML file. ucat does not read
  git: a script of your own can find the deleted files with `git diff --name-status`. The table name is the `name:`
  inside the file, so read it from the old version with `git show <base>:<path>`.

## Machine-readable output

`ucat plan -t prod --json` prints the plan as JSON, for a bot that comments on the pull request, and
`ucat apply -t prod --auto-approve --json` prints the result (`applied`, `failed`, `skipped`). See
[Commands](commands.md#json-output). The contract is versioned and in 1.x only grows.

## Large projects

`plan` and `pull` read one list call per schema, and their time barely grows with the number of tables. `apply` runs the
statements one after another (about 1.6 s for a simple `CREATE TABLE` on a serverless 2X-Small warehouse), so a first
apply of a thousand tables takes tens of minutes. Use `--schema` and `--table` to apply a part. Two `ucat` processes with
different selectors can run in parallel (measured: about 2x with two processes and about 4x with four), with service
principal credentials. Parallel `apply` inside ucat is planned, see the [Roadmap](../ROADMAP.md).
