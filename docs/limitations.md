# Limitations

What ucat v1 does and does not do, stated plainly.

## In v1

ucat manages **managed tables** of Unity Catalog:

- `CREATE TABLE`, adding, altering and dropping columns, table and column comments, nullability
- `SET` and `UNSET TBLPROPERTIES`
- Liquid clustering (`CLUSTER BY` columns or `AUTO`); partitioning at creation
- Renaming tables and columns by hint (`rename_from`)
- Dropping tables (`drop: true`, `ucat drop`)
- `pull` to write YAML from existing tables

## Not in v1

1. **Only managed tables are seen.** Views, external tables, streaming tables and materialized views are invisible to
   ucat. If a YAML table has the name of one of them, `plan` shows `create` and `apply` fails with an SQL error.
2. **No catalogs or schemas are created.** A schema must exist in Unity Catalog before ucat uses it.
3. **No views, tags, owners, grants, primary and foreign keys, row filters or column masks.**
4. **No change of a column type**, except `varchar(n)`/`char(n)` to `STRING`. Anything else (`INT` to `BIGINT`, a wider
   `DECIMAL`, nested types) is an error at planning time. For ways around it see
   [Troubleshooting](troubleshooting.md#-changing-the-type-from-x-to-y-is-not-supported-in-v1).
5. **No new `NOT NULL` column on an existing table.** Add it as nullable, fill it, then set `nullable: false`.
6. **Partitioning is set only at creation.** A change on an existing table is a warning.
7. **Clustering on a nested field** (`a.b`) is not supported.
8. **Renaming or dropping a column needs column mapping.** This is a Databricks requirement for Delta tables
   ([column mapping](https://docs.databricks.com/aws/en/delta/delta-column-mapping)). ucat expects
   `delta.columnMapping.mode: name` and does not turn it on itself, because enabling it upgrades the table protocol and
   older readers can no longer read the table.
9. **Properties are one-way, comments and nullability are authoritative, column order is not managed.** See
   [Gotchas](concepts.md#gotchas).
10. **`apply` is sequential.** One statement at a time. See [CI/CD](ci-cd.md#large-projects) for large projects.
11. **Names.** Unity Catalog allows names of up to 255 characters, without periods, spaces, slashes or control
    characters, and stores them in lower case ([naming rules](https://docs.databricks.com/aws/en/sql/language-manual/sql-ref-names)).
    ucat adds two rules of its own: logical names (catalog, schema, target) use only letters, digits, `_` and `-`,
    because they become file names, and the real name of a catalog or a schema must be lower case.
12. **Column types are checked by shape, not fully parsed.** A type that passes `validate` but is invalid in Databricks
    fails when `apply` runs it. See [Table YAML](table-yaml.md#column-types).
13. **Permissions are not checked up front.**
14. **No saved plan and no lock.** `apply` computes a new plan when it runs, and two runs against one target are not
    serialized. Use one pipeline per target and the concurrency control of your CI. See
    [CI/CD](ci-cd.md#concurrent-runs).

## Requirements

- A SQL warehouse for `apply` and `drop`; Databricks Runtime 14.3 or newer for liquid clustering. `CLUSTER BY AUTO`,
  `VARIANT` and the type change need newer versions.
- Linux, macOS and Windows (amd64; arm64 on macOS and Linux).

## What comes next

See the [Roadmap](../ROADMAP.md). It is a plan, not a promise: open an issue to say what you need.
