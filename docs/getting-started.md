# Getting started

From install to your own project in about five minutes. For a walkthrough on ready tables, with the real output of every
step, use [the quickstart example](../examples/quickstart/README.md).

## Requirements

| You need | Why |
|---|---|
| A Databricks workspace with Unity Catalog, and an existing catalog and schema | ucat does not create them |
| A SQL warehouse (a serverless 2X-Small is enough) | `apply` and `drop` run SQL on it; `plan`, `pull` and `validate` need none |
| `USE CATALOG`, `USE SCHEMA`, `CREATE TABLE`, `MODIFY` on the target schema, and `CAN USE` on the warehouse | see [Authentication and permissions](authentication.md) |
| The Databricks CLI, for the interactive login | `databricks auth login`. In CI use a token or service principal variables: no CLI needed |
| Go 1.26 or newer | only to build from source with `go install` |

## 1. Install

With Homebrew (macOS and Linux):

```bash
brew install ninilich/ucat/ucat
```

Or download the binary for Linux, macOS (amd64, arm64) or Windows (amd64) from
[GitHub Releases](https://github.com/ninilich/ucat/releases) and put it on your `PATH`. Every release has a
`checksums.txt` with a cosign signature (`checksums.txt.sigstore.json`).

Or build it from source (Go 1.26 or newer):

```bash
go install github.com/ninilich/ucat@latest
```

Check the install:

```bash
ucat version
```

## 2. Authenticate

ucat uses the Databricks SDK credential chain. It has no login of its own. The quickest way for a developer, with the
[Databricks CLI](https://docs.databricks.com/aws/en/dev-tools/cli/install):

```bash
databricks auth login --host https://<workspace>.cloud.databricks.com
```

ucat takes the workspace host from the target (`targets/<name>.yml`, written by `ucat init`) and uses the login you made
for that host, so you need no profile name. With several environments, log in once per host. See
[Several environments](authentication.md#several-environments).

In CI use a service principal through environment variables. See [Authentication and permissions](authentication.md).

## 3. Create a project

```bash
mkdir my-tables && cd my-tables
ucat init
```

`init` asks for the project name, a logical catalog and schema, the target names and, per target, the workspace host,
the Unity Catalog catalog name and the SQL warehouse ID. It creates:

```
ucat.yml
catalogs/main.yml
targets/dev.yml
tables/
```

A **target** is an environment (`dev`, `prod`). Each target names its own workspace, warehouse and catalog. Table files
are the same for every target. See [Project layout](project-layout.md).

## 4. Import existing tables (optional)

```bash
ucat pull -t dev
```

writes one YAML file per managed table of the schemas listed in `catalogs/*.yml` under `tables/<schema>/<table>.yml`.
Existing files are kept unless you pass `--force`.

## 5. Plan and apply

Add or edit a table file under `tables/` (the fields are in [Table YAML](table-yaml.md)), then:

```bash
ucat plan -t dev      # reads Unity Catalog and prints what apply would run; nothing is changed
ucat apply -t dev     # runs the changes one by one on the warehouse
```

Add `--show-sql` to `plan` to see the SQL of each change. A plan with a destructive change (drop table, drop column)
asks you to type `yes`. If you press Ctrl+C, the running step is finished and no new one starts. Renames and drops are
explicit: see [Concepts](concepts.md#renames).

## Next

- [The quickstart example](../examples/quickstart/README.md): a ready project to create, change, rename and clean up in
  five minutes, with the real output of every step.
- [Concepts](concepts.md): why there is no state file and what ucat manages.
- [Table YAML](table-yaml.md): every field.
- [Commands](commands.md): every flag and exit code.
- [CI/CD](ci-cd.md): plan on pull request, apply on merge.
