# Table YAML

One file per table. The file name and directory are conventions (`tables/<schema>/<table>.yml`); ucat reads every `.yml`
file under `tables_dir`, and the table is identified by `schema` and `name` inside the file.

```yaml
name: orders
# rename_from: orders_old          # rename the table; remove after apply
schema: silver                      # logical schema name, declared in catalogs/*.yml
comment: "Raw orders from the source system"

columns:
  - name: id
    type: BIGINT
    nullable: false
    comment: "Primary key"
  - name: customer_id
    type: BIGINT
    nullable: false
  - name: amount
    type: DECIMAL(18,2)
  - name: status
    # rename_from: order_status     # rename the column; remove after apply
    type: STRING
  - name: items
    type: ARRAY<STRUCT<product_id:BIGINT,qty:INT>>

clustering:
  columns: [customer_id, status]    # or: auto: true

tblproperties:
  delta.enableChangeDataFeed: "true"
```

## Fields

| Field | Required | Meaning |
|---|---|---|
| `name` | yes | Table name. Compared without regard to case. |
| `schema` | yes | Logical schema name from `catalogs/*.yml`. |
| `comment` | no | Table comment. Missing means empty. |
| `columns` | yes | List of columns (see below). At least one. |
| `clustering` | no | Liquid clustering: `columns: [..]` or `auto: true`. |
| `partitioning` | no | Hive-style partitioning: `columns: [..]`. Set at creation only. |
| `tblproperties` | no | Table properties; values are strings. |
| `rename_from` | no | Previous table name; renames the table. |
| `drop` | no | `true` drops the table on `apply`. |

### Columns

| Field | Required | Meaning |
|---|---|---|
| `name` | yes | Column name, unique in the table without regard to case. |
| `type` | yes | Column type (see below). |
| `nullable` | no | `false` makes the column `NOT NULL`. Missing means `true`. |
| `comment` | no | Column comment. Missing means empty. |
| `rename_from` | no | Previous column name; renames the column. |

### Column types

`TINYINT`, `SMALLINT`, `INT`, `BIGINT`, `FLOAT`, `DOUBLE`, `DECIMAL(p,s)`, `STRING`, `BOOLEAN`, `DATE`, `TIMESTAMP`,
`TIMESTAMP_NTZ`, `BINARY`, `VARIANT`, `GEOGRAPHY(..)`, `GEOMETRY(..)`, `INTERVAL ..`, and the nested `ARRAY<..>`, `MAP<..,..>`, `STRUCT<name:type,..>`.

- Types are compared ignoring case and whitespace: `DECIMAL(10, 2)` equals `decimal(10,2)`. A bare `DECIMAL` equals
  `DECIMAL(10,0)`, as in Spark.
- ucat checks the shape of a type (brackets, nesting), not the values inside `DECIMAL(p,s)` or `GEOGRAPHY(srid)`; Databricks
  rejects a wrong value when `apply` runs. See the Databricks [data types](https://docs.databricks.com/aws/en/sql/language-manual/sql-ref-datatypes).
- Write `STRING`, not `VARCHAR(n)` or `CHAR(n)`. `pull` writes `STRING` for such columns.
- The type of an existing column cannot be changed in v1, except from `varchar(n)`/`char(n)` to `STRING`. Other changes
  (`INT` to `BIGINT`, a wider `DECIMAL`) fail at planning with an explicit message. Type widening is planned for v2.

### Comments and nullability: the YAML is the full description

A missing `comment` means "no comment": if Unity Catalog has one, the next `plan` shows `unset comment` and `apply`
removes it. A missing `nullable` means `true`: for a `NOT NULL` column in Unity Catalog `plan` shows
`nullable false -> true`. `pull` writes every comment and `nullable: false`, so the loop `pull`, edit, `plan` does not
lose anything.

A **new** column with `nullable: false` in an existing table is rejected (Databricks cannot `ADD COLUMN ... NOT NULL`).
Add it as nullable, fill it with data, then set `nullable: false`.

### Clustering and partitioning

- `clustering.columns: [a, b]` runs `CLUSTER BY (a, b)`; `clustering.auto: true` runs `CLUSTER BY AUTO`.
- `clustering` and `partitioning` are mutually exclusive; so are `clustering.auto` and `clustering.columns`.
- Clustering on a nested field (`a.b`) is not supported in v1. `pull` leaves the block out with a warning.
- Partitioning can only be set when the table is created. A change on an existing table is a warning, not an `ALTER`.
- **Always declare `clustering` explicitly.** A file without a `clustering` block means no clustering. Databricks adds
  automatic liquid clustering to new managed tables by default ([automatic upgrades](https://docs.databricks.com/aws/en/tables/automatic-upgrades)), so a table created from such a
  file can come back with `clustering: {auto: true}`, and the next `plan` shows `drop clustering`; ucat prints a warning
  that explains it. Write `clustering: {columns: [...]}` for the columns you want, or `clustering: {auto: true}` to keep
  the automatic choice. If you really want no clustering, leave the block out and apply once: the plan drops it and the
  next plan is empty.

### Table properties

ucat manages only the properties you list. A property that exists in Unity Catalog but not in the YAML is left alone
(Delta and Databricks set many system properties). Removing a key from the YAML does not unset it. To unset, say so:

```yaml
tblproperties:
  delta.enableChangeDataFeed: "true"
  delta.autoOptimize.optimizeWrite: null   # UNSET this property
```

`pull` writes no properties unless you ask (`--tblproperties 'delta.*'`).

### Renames

```yaml
name: orders_v2
rename_from: orders          # ALTER TABLE ... RENAME TO
schema: silver
columns:
  - name: state
    rename_from: status      # ALTER TABLE ... RENAME COLUMN
    type: STRING
```

`rename_from` works inside one schema. Renaming or dropping a column needs `delta.columnMapping.mode: name`. After a
successful `apply` the old name is gone, and `plan` warns that the hint can be removed. ucat never edits YAML.

### Deleting a table

```yaml
name: orders
schema: silver
drop: true
```

`ucat apply --auto-approve` runs `DROP TABLE` on it. Delete the file afterwards.

### Your own keys

Keys that start with `x-` are ignored in a table and in a column. Any other unknown key is an error.
