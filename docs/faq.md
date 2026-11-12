# FAQ

## Why not just use Terraform?

You can, and the two work well together: Terraform for catalogs, schemas, grants and warehouses, ucat for the tables
inside them. ucat has no state file and shows the SQL it will run. See the [comparison](https://ninilich.github.io/ucat/#compare).

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
