# Concepts

## Desired state and live state

You write the **desired state** of your tables in YAML. ucat reads the **live state** from Unity Catalog (one REST
call per schema) and works out the difference. There is no list of migrations and no state file: if someone changes a
table outside ucat, the next `plan` shows it.

```
YAML files  ->  ucat plan  <-  Unity Catalog (live)
                   |
                   v
            changes (SQL)  ->  ucat apply
```

## What ucat manages

ucat manages only what you declare:

- A table with no YAML file is **unmanaged**. ucat never touches it.
- In a table, only the properties and columns you declare are managed: see [Gotchas](#gotchas).

v1 manages **managed tables** in existing schemas. See [Limitations](limitations.md).

## Gotchas

- **Comments and nullability are authoritative.** A missing `comment` means "empty" and a missing `nullable` means
  `true`. The YAML is the full description of both, so a comment added in Unity Catalog outside ucat (in the Databricks
  interface, in a notebook, by a colleague) is removed by the next `apply`, and `plan` shows it as a change.
- **Table properties are one-way.** ucat sets the properties you list and leaves all others alone, because Databricks and
  Delta add many system properties of their own. So removing a key from the YAML does not unset it. To unset one, write
  the key with the value `null`.
- **Declare `clustering` explicitly.** A file without a `clustering` block means no clustering, while Databricks may
  assign automatic clustering to a new table. See [Table YAML](table-yaml.md#clustering-and-partitioning).
- **A rename hint stays until you remove it.** After the apply `plan` reminds you; ucat never edits your YAML.
- **The order of columns is not managed.** If the YAML and the table list the columns in a different order, `plan` shows
  no change. A new column is added at the end of the table, whatever its place in the YAML. ucat does not reorder columns.

## One table, one file

One YAML file per table, kept in git next to your code. The file name and the directory layout are conventions; ucat
reads every `*.yml` under `tables_dir`, and the table name is the `name:` inside the file.

## Logical names and targets

Table files use **logical** names for schemas (`schema: silver`). Catalog files map a logical name to a real Unity
Catalog name, and a **target** can override that mapping per environment:

```
tables/silver/orders.yml     schema: silver
catalogs/main.yml            silver -> uc_name: silver
targets/dev.yml              catalog main -> uc_name: dev
                             => dev.silver.orders
targets/prod.yml             catalog main -> uc_name: prod
                             => prod.silver.orders
```

The environment is fully defined by the target. There is no "promote" command: each environment is a separate
`plan` and `apply` (`ucat apply -t prod`). See [Project layout](project-layout.md).

## Renames

A rename is a hint, not a guess. Without a hint, a column that disappeared from the YAML is a **drop** and a new one is
an **add**. With `rename_from: old_name` ucat runs `RENAME COLUMN` (or `RENAME TO` for a table). After the apply, the
source no longer exists in Unity Catalog and `plan` tells you the hint can be removed. ucat never edits your YAML.

Renaming or dropping a column needs `delta.columnMapping.mode: name` on the table. Preflight checks refuse the plan
otherwise. Turning column mapping on upgrades the table protocol (older readers can no longer read the table), so ucat never turns it on by
itself.

## Drops

Deleting is always explicit:

- `drop: true` in a table file drops the table on `apply`.
- `ucat drop -t <target> --schema <s> --table <t> --auto-approve` drops a table without a YAML file (for pipelines that
  detect deleted files with `git diff`).

A destructive plan asks for `yes`, or `--auto-approve` in CI.

## Plan, preflight, apply

1. **Load and validate** the project files (no network).
2. **Diff** the YAML against the live state: a list of changes.
3. **Order** the changes (create and rename first, then properties, column changes, drops).
4. **Preflight**: check what can fail later (for example a column drop without column mapping).
5. **Apply**: run the SQL one change at a time on the warehouse. The first failure stops the run and the exit code is 1.

The state is live, so a run that stopped halfway is continued by running `apply` again.

There is no saved plan: `apply` computes its own plan when it runs, so it applies what the live state and the files say
at that moment, not what an earlier `plan` showed. See [CI/CD](ci-cd.md#plan-and-apply-are-separate-runs).

Exit codes: see [Commands](commands.md#exit-codes).
