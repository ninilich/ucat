# Roadmap

A plan, not a promise. Priorities follow what users report: open an issue (or add a +1 to an existing one) to say what
matters to you. In 1.x only optional YAML keys and JSON fields are added; changes that break existing files wait for v2.

## v1.0

Managed tables of Unity Catalog. See [Limitations](docs/limitations.md) for exactly what is in and out.

## v1.x

- `DROP TABLE IF EXISTS` by default, so a repeated run does not fail
- A new column is added where it stands in the YAML, not at the end of the table
- `ucat drop` checks that the table is a managed table

## v2

The scope of v2 may change. Not in order of priority.

**More kinds of objects**
- Create schemas (`CREATE SCHEMA`)
- External tables
- Volumes

**Governance and retention**
- Tags on tables and columns
- Row expiration with auto time-to-live (`DELETE ROWS ... DAYS AFTER ...`)

**Fewer surprises, less waiting**
- Comments: choose whether a missing comment keeps or removes the comment that exists in Unity Catalog
- Wider column type changes (type widening), for example `INT` to `BIGINT`
- Faster `apply` for large projects: several statements at once

## Ideas

- Primary and foreign keys, column masks and row filters
- A saved plan and a lock between runs

## Not planned

- **`OPTIMIZE ... ZORDER BY`.** Z-ordering is an action you run on the data from time to time (Databricks describes it
  as incremental and not idempotent), not a state of the table, so it does not fit a declarative model. Databricks
  recommends liquid clustering for new tables and says it cannot be combined with Z-ordering. ucat manages liquid
  clustering: declare `clustering` in the table file.
- **Streaming tables and materialized views.** A streaming table is defined in the source code of a pipeline and is
  owned and updated by that one pipeline; a materialized view is likewise defined by a query and refreshed by
  Databricks. Their lifecycle belongs to the pipeline, not to a table definition in YAML, so they are deployed with the
  pipeline (for example with Declarative Automation Bundles). ucat is for the tables you define yourself.
