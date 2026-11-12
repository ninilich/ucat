# Architecture

For contributors. A short map of the code; the design decisions are explained in the code comments next to the code.

## Data flow

```
LoadConfig -> ValidateLocal -> DiffSchema -> planner.Plan -> preflight.Run -> executor.Execute
```

- **Load and validate** (`internal/config`): read `ucat.yml`, `catalogs/`, `targets/` and the table files strictly,
  resolve logical names to Unity Catalog names for a target, run the local checks. No network.
- **Live state** (`internal/live`, `internal/backend`, `internal/uc`): one list call per schema through the backend
  interface; at most 8 in parallel.
- **Diff** (`internal/differ`): desired tables against live tables gives a list of `change.Change` values (create table,
  add column, set properties, ...). The differ does not know which backend runs them.
- **Plan** (`internal/planner`): stable ordering of the changes.
- **Preflight** (`internal/preflight`): checks that can fail later, for example a column drop without column mapping.
- **Execute** (`internal/executor`): runs `Change.SQL()` one by one through the backend, with progress, timing and
  interruption handling. It returns a `Result` (total, applied, skipped, failed, aborted).
- **Output** (`internal/output`): plan text, JSON, progress.

## Packages

| Package | Role |
|---|---|
| `cmd` | cobra commands, flags, the pipeline for `plan` and `apply`, selectors, signals |
| `internal/config` | Loading, strict parsing, validation, name rules, resolving catalogs and targets |
| `internal/change` | The `Change` interface and its implementations (one per kind of change) |
| `internal/differ` | Desired against live, type normalization, rename handling |
| `internal/planner` | Ordering |
| `internal/preflight` | Checkers that run before `apply` |
| `internal/executor` | Running the plan |
| `internal/backend` | The `UCBackend` interface and the live table type; `backendtest` has the mock |
| `internal/uc` | The Databricks implementation (REST for state, Statement Execution API for SQL) |
| `internal/live` | Reading the live state in parallel |
| `internal/pull` | Live tables to YAML |
| `internal/output` | Printing |
| `internal/diag` | Errors and warnings with locations |
| `internal/testenv` | Isolates tests from `DATABRICKS_*` variables |

## Rules the code follows

- **A new kind of change** is wired in every place: a `Kind` constant and its name in `internal/change`, an explicit case
  in `planner.orderGroup` (an unknown kind panics), the output classification, and a row in the kind test.
- **SQL** built from YAML text escapes identifiers and string literals in one place.
- **Name rules** live in `internal/config/names.go`: case folding, logical names, Unity Catalog names, reserved Windows names.
- **Errors** go to stderr with a location (`file:line`); a command returns an error, it does not call `os.Exit`.
- **No state file.** Anything that needs the live state reads it through the backend.
- **Compatibility:** in 1.x only optional YAML keys and JSON fields are added.

## Backend interface

```go
type UCBackend interface {
    ListTables(ctx, schema) ([]*LiveTable, error)
    ApplyChange(ctx, ch change.Change) error
    CheckWarehouse(ctx) error
    GetTableProperties(ctx, catalog, schema, table) (map[string]string, error)
}
```

v1 has one implementation, Databricks. The interface is where an open-source Unity Catalog backend would plug in.
