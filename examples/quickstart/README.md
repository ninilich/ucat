# ucat quickstart

A small shop schema (`customers`, `orders`, `order_items`) that you create in Unity Catalog, change twice and clean up,
in about five minutes. To set up your own project instead, see [Getting started](../../docs/getting-started.md). Everything below was run on a real workspace; the outputs are from that run (host, catalog and
warehouse are masked).

## What you need

- A Databricks workspace with Unity Catalog, a catalog where you may create tables, and a SQL warehouse
  (a serverless 2X-Small is enough).
- Permissions in that catalog: `USE CATALOG`, `USE SCHEMA`, `CREATE SCHEMA` (once), `CREATE TABLE`, `MODIFY`, and
  `CAN USE` on the warehouse. See [Authentication and permissions](../../docs/authentication.md).
- ucat installed (`ucat version` works) and a Databricks login, for example
  `databricks auth login --host https://<workspace>.cloud.databricks.com`. See [Getting started](../../docs/getting-started.md).

ucat does not create schemas, so create the one this example uses:

```sql
CREATE SCHEMA IF NOT EXISTS your_catalog.shop;
```

## 1. Point the project at your workspace

```bash
cd examples/quickstart
cp targets/dev.yml.example targets/dev.yml
```

Edit `targets/dev.yml`: your workspace host, the id of a SQL warehouse and the catalog. `targets/dev.yml` is ignored by
git, so your values stay local.

```yaml
workspace:
  host: https://your-workspace.cloud.databricks.com
  warehouse_id: your-warehouse-id
catalogs:
  main:
    uc_name: your_catalog
```

The table files in `tables/shop/` know nothing about your catalog: they refer to the logical schema `shop`, which
`catalogs/main.yml` declares. A **target** is an environment. `targets/prod.yml.example` shows the same tables aimed at
another catalog and warehouse.

## 2. Check the project

```
$ ucat validate
✓ all local checks passed

$ ucat validate -t dev
✓ all local checks passed
✓ schema your_catalog.shop exists
✓ warehouse your-warehouse-id reachable
```

## 3. Plan and apply: create the tables

```
$ ucat plan -t dev
  `your_catalog`.`shop`.`customers`
    + create table `your_catalog`.`shop`.`customers` (5 columns)

  `your_catalog`.`shop`.`order_items`
    + create table `your_catalog`.`shop`.`order_items` (4 columns)

  `your_catalog`.`shop`.`orders`
    + create table `your_catalog`.`shop`.`orders` (5 columns)

Plan: 3 to add, 0 to change, 0 to destroy.
```

Add `--show-sql` to see the statements, for example `CREATE TABLE ... CLUSTER BY (`country`)`. Every table file declares
its `clustering` explicitly on purpose: Databricks adds automatic liquid clustering to new managed tables by default, and
a file without a `clustering` block means "no clustering", so the next plan would show `drop clustering`. Declaring it
makes the example behave the same in every workspace (see [Table YAML](../../docs/table-yaml.md#clustering-and-partitioning)).
Then apply:

```
$ ucat apply -t dev
...
  [1/3] applied in 2.9s: `your_catalog`.`shop`.`customers` - create table `your_catalog`.`shop`.`customers` (5 columns)
  [2/3] applied in 1.9s: `your_catalog`.`shop`.`order_items` - create table `your_catalog`.`shop`.`order_items` (4 columns)
  [3/3] applied in 1.8s: `your_catalog`.`shop`.`orders` - create table `your_catalog`.`shop`.`orders` (5 columns)

Apply complete in 6.6s.
```

A plan right after the apply is empty, because Unity Catalog now matches the files:

```
$ ucat plan -t dev
No changes. Infrastructure is up-to-date.
```

## 4. Change a table: a column, clustering, a property

`steps/1-add-column/orders.yml` is `orders` with a new column `discount`, a different clustering (`customer_id` instead
of `created_at`) and a table property. Copy it over the original:

```bash
cp steps/1-add-column/orders.yml tables/shop/orders.yml
ucat plan -t dev --show-sql
```

```
  `your_catalog`.`shop`.`orders`
    ~ set 1 tblproperties
    ~ add column "discount" DECIMAL(5,2)
    ~ set clustering by (customer_id)

Plan: 0 to add, 3 to change, 0 to destroy.
set 1 tblproperties
    ALTER TABLE `your_catalog`.`shop`.`orders` SET TBLPROPERTIES ('delta.enableChangeDataFeed'='true')

add column "discount" DECIMAL(5,2)
    ALTER TABLE `your_catalog`.`shop`.`orders` ADD COLUMN `discount` DECIMAL(5,2) COMMENT 'Percent off the amount'

set clustering by (customer_id)
    ALTER TABLE `your_catalog`.`shop`.`orders` CLUSTER BY (`customer_id`)
```

You wrote the end state; ucat worked out the three `ALTER` statements. Apply them:

```
$ ucat apply -t dev
...
  [1/3] applied in 1.4s: `your_catalog`.`shop`.`orders` - set 1 tblproperties
  [2/3] applied in 1.1s: `your_catalog`.`shop`.`orders` - add column "discount" DECIMAL(5,2)
  [3/3] applied in 878ms: `your_catalog`.`shop`.`orders` - set clustering by (customer_id)

Apply complete in 3.4s.
```

## 5. Rename a table

`steps/2-rename-table/order_lines.yml` is `order_items` under a new name, with `rename_from: order_items`. Replace the
old file with it:

```bash
rm tables/shop/order_items.yml
cp steps/2-rename-table/order_lines.yml tables/shop/
ucat plan -t dev
```

```
  `your_catalog`.`shop`.`order_lines`
    ~ rename table order_items → order_lines

Plan: 0 to add, 1 to change, 0 to destroy.
```

A rename, not a drop and a create: the data stays. Apply it. After that `plan` tells you the hint is no longer needed:

```
$ ucat plan -t dev
No changes. Infrastructure is up-to-date.
warning: table "shop.order_lines": rename_from "order_items" can be removed after apply (tables/shop/order_lines.yml)
```

Delete the `rename_from` line from `tables/shop/order_lines.yml`; the warning is gone. ucat never edits your YAML.

Renaming a **column** works the same way (`rename_from` next to the column), but it needs `delta.columnMapping.mode: name` on
the table, which upgrades the table protocol (older readers can no longer read the table). See [Table YAML](../../docs/table-yaml.md).

## 6. Import tables that already exist (optional)

`ucat pull` writes the YAML for tables that are already in Unity Catalog. In an empty directory with the same
`ucat.yml`, `catalogs/` and `targets/`:

```
$ ucat pull -t dev --catalog main --schema shop
✓ created  tables/shop/customers.yml
✓ created  tables/shop/order_lines.yml
✓ created  tables/shop/orders.yml
```

A plan right after the pull is empty, so you can adopt tables you already have without changing them.

## 7. Clean up

`order_items` no longer exists, it is `order_lines` now:

```bash
ucat drop -t dev --schema shop --table customers --auto-approve
ucat drop -t dev --schema shop --table orders --auto-approve
ucat drop -t dev --schema shop --table order_lines --auto-approve
```

and, if you like, `DROP SCHEMA your_catalog.shop`.

## Where next

- [Concepts](../../docs/concepts.md): why there is no state file.
- [Table YAML](../../docs/table-yaml.md): every field.
- [CI/CD](../../docs/ci-cd.md): plan on a pull request, apply on merge, one target per environment.
