# Authentication and permissions

## Credentials

ucat has no login and no `--profile` or `--token` flags. It creates a Databricks SDK client and lets the SDK find the
credentials with its default chain. Anything that works for the Databricks CLI or SDK works for ucat. The Databricks CLI
is needed only for the interactive login (`databricks auth login`); the other methods do not use it, so CI needs no CLI.
The methods the SDK tries, in order (the first that works wins):

| Method | Typical use | Configured by |
|---|---|---|
| Personal access token | Quick CI setup | `DATABRICKS_HOST` + `DATABRICKS_TOKEN` (or `token` in a profile) |
| OAuth machine-to-machine | Service principal, recommended for CI/CD | `DATABRICKS_CLIENT_ID` + `DATABRICKS_CLIENT_SECRET` (plus the host) |
| Databricks CLI | Interactive development | `databricks auth login`; the Databricks CLI must be installed. Not for parallel jobs, see [Troubleshooting](troubleshooting.md#cannot-configure-default-credentials) |
| OIDC federation (GitHub, Azure DevOps, ...) | CI without stored secrets | SDK variables; see the Databricks documentation |
| Azure (managed identity, client secret, `az login`) | Azure | `ARM_*` variables |
| Google | GCP | SDK variables |

`DATABRICKS_AUTH_TYPE` pins one method. A named profile from `~/.databrickscfg` is selected with
`DATABRICKS_CONFIG_PROFILE`. For the details see "Databricks client unified authentication" in the Databricks
documentation.

## Several environments

The workspace host of a target comes from `targets/<t>.yml`. For a developer the credentials come from the Databricks
CLI: ucat asks it for the login of that host. So log in once per workspace, and each target uses its own:

```bash
databricks auth login --host https://dev.cloud.databricks.com
databricks auth login --host https://prod.cloud.databricks.com

ucat plan -t dev     # uses the login for the host in targets/dev.yml
ucat plan -t prod    # uses the login for the host in targets/prod.yml
```

This needs the Databricks CLI installed and its `~/.databrickscfg`, and no `DATABRICKS_TOKEN` or other variable that comes
earlier in the credential chain (see the table above), because it would win. Set `DATABRICKS_CONFIG_PROFILE` only when
two profiles point to the same host with different identities, or when the target has no host.

## Workspace host and SQL warehouse

These two can also come from the project files. The first value that is set wins:

| Value | Order |
|---|---|
| Workspace host | `targets/<t>.yml`, then `ucat.yml`, then `DATABRICKS_HOST` |
| SQL warehouse | `targets/<t>.yml`, then `ucat.yml`, then `DATABRICKS_WAREHOUSE_ID` |

A value in a project file wins over the environment, so a pipeline-level variable cannot redirect a `prod` target to
another workspace. If the variable is set and differs from the file, ucat prints a warning that the file value is
used. To select the warehouse or the workspace per job, leave them out of the files and set the variables. To run against another
workspace, use a different target.

## SQL warehouse

`plan`, `pull` and `validate` use only the REST API and need no warehouse. `apply` and `drop` run SQL and need one.
Without it they stop before the prompt and before any SQL:

```
apply requires a SQL warehouse: set workspace.warehouse_id in targets/dev.yml (or ucat.yml), or DATABRICKS_WAREHOUSE_ID
```

`validate -t` checks that the warehouse is reachable; without a warehouse it warns and skips the check. A serverless
2X-Small warehouse is enough: the statements are small DDL.

## Minimum permissions

For the user or service principal that runs ucat, by operation:

| What ucat does | What it needs |
|---|---|
| `plan`, `pull`, `validate` | No SQL warehouse. Unity Catalog read access to the schemas and tables. |
| Create a table | `USE CATALOG`, `USE SCHEMA` and `CREATE TABLE` on the schema |
| Add, change or drop a column; set or unset table properties | `MODIFY` on the table |
| Rename a table or a column; change clustering | Ownership of the table (see the note below) |
| Drop a table (`drop: true`, `ucat drop`) | `MANAGE` on the table, or ownership of the table, the schema or the catalog |
| Every `apply` and `drop` | `CAN USE` on the SQL warehouse |

The table follows the Databricks documentation of [`ALTER TABLE`](https://docs.databricks.com/aws/en/sql/language-manual/sql-ref-syntax-ddl-alter-table)
and [`DROP TABLE`](https://docs.databricks.com/aws/en/sql/language-manual/sql-ref-syntax-ddl-drop-table), checked on
9 November 2026. `ALTER TABLE` lists `MODIFY` for columns and table properties and says that all other operations
require ownership of the table. It does not name renames and `CLUSTER BY`, so for them we apply that rule. If in doubt,
try it on your workspace. ucat does not check permissions up front: the only reliable check is to try the operation, and
the SQL error is shown. ucat does not create catalogs or schemas, so it needs no permission for that.

The simplest setup is to let the service principal create the tables, so it owns them. For tables created by someone
else, grant what the table needs and, if ucat should rename them or change their clustering, make the principal the owner.
