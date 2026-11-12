# Project layout

A ucat project is a directory with `ucat.yml` at its root. ucat finds the root by walking up from the current
directory, so you can run it from any subdirectory.

```
my-tables/
  ucat.yml                  project settings
  catalogs/
    main.yml                logical catalog -> Unity Catalog names
  targets/
    dev.yml                 one file per environment
    prod.yml
  tables/
    silver/
      orders.yml            one file per table
      customers.yml
```

Only files with a lower-case `.yml` extension are read. `*.yaml` and `*.YML` are ignored, and ucat warns about them.
A second YAML document in a file (after `---`) is ignored, with a warning.

## ucat.yml

```yaml
project: my-platform
tables_dir: tables          # where the table files are; relative to the project root; default "tables"
workspace:
  warehouse_id: abc123      # default SQL warehouse; a target can override it
```

`tables_dir` must be relative. `..` is allowed, so several projects can share one directory of tables.

| Field | Required | Meaning |
|---|---|---|
| `project` | no | Name of the project, for people |
| `tables_dir` | no | Where the table files are; default `tables` |
| `workspace.host` | no | Workspace URL; a target can override it |
| `workspace.warehouse_id` | no | Default SQL warehouse; a target can override it |

An empty `ucat.yml` is valid.

## `catalogs/<name>.yml`

One file per **logical** catalog. `name` is authoritative (ucat warns when it differs from the file name).

```yaml
name: main                  # logical name, used in the project
uc_name: prod               # the real Unity Catalog catalog; a target can override it
schemas:
  silver:
    uc_name: silver         # optional; defaults to the key
  gold:
    uc_name: gold
```

Schemas are declared here, and table files refer to them by the logical name (`schema: silver`). A schema must exist in
Unity Catalog already: ucat does not create schemas.

| Field | Required | Meaning |
|---|---|---|
| `name` | yes | Logical name of the catalog |
| `uc_name` | in the file or in each target | The real catalog name |
| `schemas.<name>` | for every schema that tables use | A logical schema; a table with an unknown `schema:` fails `validate` |
| `schemas.<name>.uc_name` | no | The real schema name; default is the key |

## `targets/<name>.yml`

One file per environment. `-t <name>` selects it. There is no default target, so `plan`, `apply`, `pull` and `drop`
need `-t`. An empty target file is valid.

```yaml
# targets/prod.yml
workspace:
  host: https://prod.cloud.databricks.com   # optional; otherwise the SDK takes it from the environment or a profile
  warehouse_id: abc123prod
catalogs:
  main:
    uc_name: prod                            # the physical catalog of this environment
    schemas:
      silver:
        uc_name: silver_prod                 # optional per-schema override
```

Give every target its **own** `uc_name` for each logical catalog. Targets that do not set their own all use the catalog
of the catalog file, so their environments share one catalog: `plan`, `apply` and `validate` warn about it.

| Field | Required | Meaning |
|---|---|---|
| `workspace.host` | no | Workspace URL of this environment |
| `workspace.warehouse_id` | no | SQL warehouse; needed for `apply` and `drop` |
| `catalogs.<logical>.uc_name` | no | The real catalog of this environment |
| `catalogs.<logical>.schemas.<s>.uc_name` | no | Per-schema override |

A target can also hold personal sandboxes (`targets/sandbox_username.yml`); add them to `.gitignore` if they are local.

Where the host and the warehouse come from when several places set them: see
[Authentication](authentication.md#workspace-host-and-sql-warehouse).

## Names

Logical names (catalog, schema, target) become file and directory names, so they use only letters, digits, `_` and `-`
(at most 100 characters) and may not be a reserved Windows device name (`CON`, `PRN`, `AUX`, `NUL`, `COM1`-`COM9`,
`LPT1`-`LPT9`). Real Unity Catalog names (`uc_name`) must be lower case, without whitespace, dots, slashes or control
characters.
Table and column names are compared without regard to case, as Unity Catalog does.

## The table files

See [Table YAML](table-yaml.md). The directory layout under `tables_dir` is a convention (`tables/<schema>/<table>.yml`);
ucat reads every `.yml` file under it, and the table name is the `name:` inside the file.

## Compatibility

In ucat 1.x new YAML keys are only added as optional ones. ucat reads files strictly: an unknown key is an error, with
a hint to upgrade ucat in case the file was written for a newer version.

### `x-` keys

Keys that start with `x-` are ignored at the top level of `ucat.yml`, a catalog file, a target file, a catalog entry
inside a target, a table and a column, so you can keep your own notes or YAML anchors there. They are not accepted
anywhere else (for example under `workspace`, `clustering` or a schema).
