# FAQ

## Why not just use Terraform?

You can, and the two work well together: Terraform for catalogs, schemas, grants and warehouses, ucat for the tables
inside them. ucat has no state file and shows the SQL it will run. See the [comparison](https://ucat.dev/#compare).

## Why not Liquibase?

Liquibase runs versioned migrations: you write each change as a changeset and every database keeps a history of what was
applied. That fits migrations of data, a fixed order of changes and many kinds of databases. ucat is declarative: the
YAML is the table as it should be, `plan` compares it with Unity Catalog and works out the ALTER statements, so a change
made by hand shows up in the next plan. There is no history table; git and the plan are the record. Use Liquibase if you
need migrations of data or one tool for several databases; use ucat if you want to read the whole table in one file
and review the SQL of a change in a pull request. See the [comparison](https://ucat.dev/#compare).

## Why not define tables in a pipeline?

In a Lakeflow pipeline (Spark Declarative Pipelines, formerly Delta Live Tables) the pipeline owns its tables: the schema
comes from the query, and the table is created and refreshed with that code. That is the right model for streaming tables
and materialized views. ucat is for the other kind: a table that has its own definition, a file in git that you review
like a contract, and that any job, pipeline or notebook can write into. See
[Who owns a table](concepts.md#who-owns-a-table).

## Does ucat replace Lakeflow?

No. Pipelines stay the place for streaming tables and materialized views, which ucat does not manage. ucat is for the
tables you define yourself, for example the tables your jobs load data into. The two work side by side.

## Someone changed a table by hand. What happens?

The next `plan` shows it, because ucat compares the YAML with the live table, not with a previous run. What `apply` does
with each kind of change is in [Gotchas](concepts.md#gotchas).

## How do I roll back a change?

Revert the commit in git and run `plan` and `apply` again: ucat computes the way back from the live state. Two caveats:

- Going back over an added column means dropping it. That needs `delta.columnMapping.mode: name` and the data of the
  column is gone.
- A column type change is not supported in either direction. See
  [the workaround](troubleshooting.md#-changing-the-type-from-x-to-y-is-not-supported-in-v1).

A dropped table is not restored by ucat; recreate it from its YAML and restore the data by your usual means.

## A table exists in Unity Catalog but has no YAML file. Will ucat touch it?

No. A table without a file is unmanaged and is never changed or dropped. To start managing it, run
`ucat pull` to write its YAML, then review the file and commit it.

## Several teams share one schema. Can they use ucat?

Yes, if each table has one owner. ucat acts only on tables that have a file in the project it runs from, so two projects
can use the same schema as long as they declare different tables. There is no lock between runs, so do not run two
`apply` against one target at the same time (see [Concurrent runs](ci-cd.md#concurrent-runs)).
