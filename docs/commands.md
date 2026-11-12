# Commands

```
ucat <command> [flags]
```

| Command | What it does | Needs `-t` | Needs a warehouse |
|---|---|---|---|
| `init` | Create a project interactively | no | no |
| `validate` | Check the project files; with `-t` also check Unity Catalog | optional | with `-t`, to check connectivity |
| `plan` | Show what `apply` would change | yes | no |
| `apply` | Make Unity Catalog match the YAML | yes | yes |
| `drop` | Drop one table without its YAML file | yes | yes |
| `pull` | Write YAML from the tables in Unity Catalog | yes | no |
| `version` | Print the version | no | no |

Flags belong to commands. `ucat --help` lists the commands; `ucat <command> --help` lists the flags of a command with
examples. The root command has only `--version`, which prints the same text as `ucat version`.

`-t, --target <name>` selects `targets/<name>.yml`. There is no default target.

## init

```bash
ucat init
```

Asks for the project name, one logical catalog and schema, the target names and, per target, the workspace host, the
Unity Catalog catalog name and the SQL warehouse ID. An empty answer takes the default in brackets. Every answer is
checked before a file is written. With one target the default Unity Catalog catalog is the logical name; with several it
is `<logical>_<target>`. It creates `ucat.yml`, `catalogs/<catalog>.yml`, `targets/<target>.yml` and the tables directory.
`init` does not roll back: after an error in the middle it lists the files it already created.

## validate

```bash
ucat validate            # local checks only
ucat validate -t dev     # plus Unity Catalog
```

Local checks cover `ucat.yml`, catalogs, targets and tables: syntax, unknown keys, names, types, duplicates, references
to schemas, mutually exclusive options. With `-t` ucat also checks that every catalog and schema exists, that the
warehouse is reachable, and reports the warnings of the diff (for example a `rename_from` that can be removed). It also
reports planning errors such as an unsupported type change. When a check fails the command exits with code 1 and
`Error: live validation failed for <check>` (for example `schema dev.silver`) naming the first failed one; a schema
with planning errors still shows `✓ schema dev.silver exists`, the errors are printed on stderr. Permissions are not
checked.

## plan

```bash
ucat plan -t dev
ucat plan -t prod --show-sql
ucat plan -t prod --schema silver --table gold.customers
ucat plan -t prod --json --detailed-exitcode
```

Reads the live state and prints the changes `apply` would run, in the order they would run. Nothing is changed.

| Flag | Meaning |
|---|---|
| `-t, --target` | Target (required) |
| `--show-sql` | Print the SQL of each change |
| `--json` | One JSON document on stdout, see below |
| `--detailed-exitcode` | Exit 0 when there are no changes, 2 when there are |
| `--schema <name>` | Only the tables of this schema; repeatable |
| `--table <schema.table>` or `<table>` | Only this table; repeatable. A bare name selects it in every schema |

The summary line counts `add` (create table), `destroy` (drop table, drop column) and `change` (everything else).

### Selectors

`--schema` and `--table` take the logical names of the YAML, without regard to case, and combine as a union. A name that
matches nothing fails before Unity Catalog is read and lists what is available. Only the selected tables are read from
Unity Catalog. A table with `drop: true` is selected like any other. A rename is selected by the new name. A live table
that is not in the YAML is never touched.

## apply

```bash
ucat apply -t dev
ucat apply -t prod --auto-approve
ucat apply -t prod --schema silver
```

Plans, runs the preflight checks, asks for confirmation if the plan has a destructive change, and runs the changes one by
one as SQL on the target's warehouse. The first failure stops the run with exit code 1. Each step line shows its
duration: `[3/12] applied in 1.6s: ...`.

Flags: `-t`, `--auto-approve`, `--show-sql`, `--json`, `--schema`, `--table` (as in `plan`).

**Ctrl+C or SIGTERM:** the running step is finished (a statement is not cut in the middle), no new step starts, ucat
prints `interrupted after step i/N; k steps not applied` and exits with 130. A second signal ends the process at once.
Run `apply` again to continue: the state is read live.

**Without a terminal** and without `--auto-approve`, a plan with a destructive change is not applied (`apply cancelled`,
exit code 1).

## drop

```bash
ucat drop -t dev --schema silver --table orders --auto-approve
```

Drops one table without looking for its YAML file. Use it only for tables ucat manages: it sends `DROP TABLE` without
checking the kind of the object. It is meant for pipelines that find deleted files themselves (see
[CI/CD](ci-cd.md#deleting-tables-from-a-pipeline)). `--schema` is the logical name. A missing table is an error
(`DROP TABLE` runs without `IF EXISTS`), so a typo in `--table` is not silently ignored; `drop: true` in a file is
different: a table that is already gone is only a warning. `--auto-approve` is required.
After Ctrl+C: before the `DROP` is sent nothing is dropped; once it is sent it is awaited. After a second signal the
outcome is unknown, so check with `ucat plan` or `ucat pull`.

## pull

```bash
ucat pull -t dev
ucat pull -t dev --catalog main --schema silver
ucat pull -t dev --force --tblproperties 'delta.*'
```

Writes the managed tables of the schemas declared in `catalogs/*.yml` as table files under `tables_dir`
(`tables/<schema>/<table>.yml`). Tables that are already defined in some file are skipped unless `--force`, which
rewrites that file. `varchar(n)`/`char(n)` columns are written as `STRING` with a warning. `tblproperties` are not
written unless selected by `--tblproperties` (comma-separated keys or prefixes ending in `*`, or `all`), because Delta and
Databricks set many system properties of their own, and applying them from your YAML could fail or upgrade the table
protocol. With `--force` a property that is already in the file is dropped from it unless you select it again.

| Flag | Meaning |
|---|---|
| `--catalog <logical>` | Only this logical catalog |
| `--schema <logical>` | Only this schema (requires `--catalog`) |
| `--force` | Overwrite the files of tables that are already defined |
| `--tblproperties <list>` | Keys to write |

## version

`ucat version` and `ucat --version` print the version, the commit and the build date. A binary built with
`go install github.com/ninilich/ucat@vX.Y.Z` reports that tag; any other build without release flags reports `dev`.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Success. `plan --detailed-exitcode`: no changes |
| 1 | Error, or the destructive prompt was declined |
| 2 | `plan --detailed-exitcode`: changes present |
| 130 | Interrupted by Ctrl+C or SIGTERM |

## Output streams

stdout carries the result: the plan, the progress of `apply`, JSON. stderr carries warnings, errors and the prompt. With
`--json`, stdout holds only the JSON document.

## JSON output

`plan --json` and `apply --json` print one JSON document. The contract is versioned by `schema_version` (currently
`1`). In 1.x fields are only added and new values of `op` may appear: ignore what you do not know.

`plan --json`:

```json
{
  "schema_version": 1,
  "summary": {"add": 1, "change": 1, "destroy": 0},
  "changes": [
    {
      "op": "add_column",
      "step": 1,
      "catalog": "dev",
      "schema": "silver",
      "table": "orders",
      "column": "discount",
      "path": "dev.silver.orders",
      "description": "add column \"discount\" DOUBLE",
      "sql": "ALTER TABLE `dev`.`silver`.`orders` ADD COLUMN `discount` DOUBLE",
      "destructive": false
    }
  ]
}
```

`op` is one of `create_table`, `rename_table`, `set_tbl_properties`, `unset_tbl_properties`, `alter_clustering`,
`alter_table_comment`, `add_column`, `rename_column`, `alter_column`, `drop_column`, `drop_table`. Optional fields:
`column`, `previous_name` (a rename), `alter` (`type`, `comment` or `nullable`, for `alter_column`). `path` has no SQL
quotes. `description` and `sql` are for people; do not parse them. An empty plan prints `"changes": []`.
When planning fails, no JSON is printed and the exit code is 1.

`apply --json`:

```json
{"schema_version": 1, "total": 4, "applied": 1, "failed": null, "skipped": 3, "aborted": "interrupted"}
```

`failed` is `null` or the change as in the plan plus `error`. `aborted` appears only when the run stopped without a failed
change: `declined` (the prompt was answered no), `input_error` (the prompt could not be read) or `interrupted`. The plan,
the progress and the prompt go to stderr.
