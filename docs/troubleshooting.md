# Troubleshooting

Messages are quoted as ucat prints them. Warnings and errors go to stderr.

## `ucat.yml not found: searched from <dir> to filesystem root`

You are not inside a ucat project. Run `ucat init`, or `cd` into the project. ucat looks in the current directory and
every parent directory.

## `plan requires -t <target>`

`plan`, `apply`, `pull` and `drop` need a target: `ucat plan -t dev` (the message names the command). `validate` without
`-t` runs only the local checks.

## `failed to read targets/<name>.yml`

`-t` takes the name of a file in `targets/` without `.yml`. Check the spelling; `ucat` has no default target.

## `apply requires a SQL warehouse: set workspace.warehouse_id ...`

`apply` and `drop` run SQL on a warehouse. Set `workspace.warehouse_id` in `targets/<t>.yml` or `ucat.yml`, or the
environment variable `DATABRICKS_WAREHOUSE_ID`. See [Authentication](authentication.md#sql-warehouse).

## `cannot configure default credentials`

The Databricks SDK found no credentials. Log in (`databricks auth login`; this needs the Databricks CLI on the `PATH`),
or set `DATABRICKS_HOST` with a token or service principal variables. If several profiles match one host, select one
with `DATABRICKS_CONFIG_PROFILE`. See [Authentication](authentication.md#several-environments).

If it happens only when several `ucat` processes start at the same moment with a Databricks CLI login that keeps its
tokens in the secure store, the processes race for the token (this is what we observed, not a documented guarantee).
Use service principal variables (`DATABRICKS_HOST`, `DATABRICKS_CLIENT_ID`, `DATABRICKS_CLIENT_SECRET`) for parallel runs.

## `workspace.warehouse_id "..." from targets/dev.yml is used; DATABRICKS_WAREHOUSE_ID (...) is ignored`

Only a warning: the value in a project file wins. See [Authentication](authentication.md#workspace-host-and-sql-warehouse).
The same warning exists for `workspace.host` and `DATABRICKS_HOST`.

## `unknown schema ref "x": not defined in any catalog config`

A table has `schema: x`, but no `catalogs/*.yml` declares a schema `x`. Add it under `schemas:` in the catalog file.

## `field "x" is not supported`

An unknown key in a YAML file (often a typo; the line number is shown). If the file was written for a newer ucat,
upgrade ucat. Your own notes go under keys that start with `x-` (see [Project layout](project-layout.md#x--keys)).

## `... changing the type from X to Y is not supported in v1`

You changed the type of an existing column. v1 plans only `varchar(n)`/`char(n)` to `STRING`. Type widening is planned
for v2. Two ways around it, depending on whether the table has column mapping (`delta.columnMapping.mode: name`, see
the next entry).

**With a new table (no column mapping needed).**

1. Add a table file with a new `name` and the new column type.
2. `apply`, then copy the data in SQL, casting the column:
   `INSERT INTO new_table SELECT ..., CAST(col AS NEW_TYPE) ... FROM old_table`.
3. Point the readers and writers at the new table.
4. Set `drop: true` in the old table file and `apply` (the drop asks for confirmation).

**In place (the table needs column mapping).**

1. Add a new column with the new type and `apply`.
2. Copy the data: `UPDATE t SET new_col = CAST(old_col AS NEW_TYPE)`.
3. Point the readers at the new column.
4. Remove the old column from the YAML and `apply`. Dropping a column needs column mapping.

## `a new NOT NULL column cannot be added to an existing table`

Databricks cannot add a column with `NOT NULL`. Add it as nullable (no `nullable` key), fill it with data, then set
`nullable: false` in a second change.

## A column drop or rename is blocked by a preflight check

Renaming or dropping a column needs `delta.columnMapping.mode: name` on the table. Add it under `tblproperties` in the
table file and run `apply` again; the property is set first. Note that turning column mapping on upgrades the table
protocol: older readers will no longer be able to read the table. ucat never turns it on by itself.

## `plan` shows `create`, but the table exists

ucat sees only **managed** tables. If the name is taken by a view or an external table, `plan` shows `create` and
`apply` fails with an SQL error. Rename or remove the other object.

## `plan` shows `drop clustering` right after `apply`

Databricks adds automatic liquid clustering to new Unity Catalog managed tables ([automatic upgrades](https://docs.databricks.com/aws/en/tables/automatic-upgrades)), and a table
file without a `clustering` block means no clustering. ucat warns about it:

```
warning: table "shop.customers": Unity Catalog assigned automatic clustering and the file has no clustering block, so apply will drop it; add "clustering: {auto: true}" to keep it (tables/shop/customers.yml)
```

Declare the clustering in the file: `clustering: {auto: true}` keeps the automatic choice, `clustering: {columns: [...]}`
picks the columns. If you want no clustering, apply once: the drop runs and the next plan is empty. See
[Table YAML](table-yaml.md#clustering-and-partitioning).

## `plan` shows a comment or nullability change you did not make

The YAML is the full description of comments and nullability (see [Gotchas](concepts.md#gotchas)). Add the comment to the
YAML, or run `ucat pull -t <target> --force` to write the live comments into the files.

## A table property did not disappear when I removed it from the YAML

Properties are one-way, see [Gotchas](concepts.md#gotchas). Set the key to `null` to unset it.

## `rename_from "old" can be removed after apply`

Only a warning: the rename is done. Delete the `rename_from` line from the file.

## `N file(s) are ignored (ucat reads only files named *.yml ...)`

ucat reads only files with a lower-case `.yml` extension. Rename `*.yaml` and `*.YML` files.

## `no table files (*.yml) found in "tables"`

The tables directory is empty or `tables_dir` points elsewhere. Run `ucat pull -t <target>` to import tables, or add table
files.

## `apply cancelled: no input`

The plan has a destructive change and there is no terminal to ask you. Pass `--auto-approve` in a pipeline.

## `apply` failed halfway

The first failed statement stops the run and the output says which step failed (`[i/N] failed ...`) and why. Steps
before it are applied. Fix the cause and run `apply` again: the state is read live, so only what is left is planned. A
run interrupted with Ctrl+C (exit code 130) is continued the same way.

## `PERMISSION_DENIED` from Databricks

ucat does not check permissions up front: the SQL error of Databricks is shown as it is. See the permissions by
operation in [Authentication](authentication.md#minimum-permissions).

## A schema does not exist

`validate -t` fails with `live validation failed for schema <catalog>.<schema>`. ucat does not create schemas: create
the schema in Unity Catalog first.

## `--schema` or `--table` matches nothing

The selectors take the logical names of the YAML, without regard to case. The error lists what is available.

## Something else

Run `ucat plan -t <target> --show-sql` to see the exact SQL, and open an issue with the output and the ucat version
(`ucat version`).
