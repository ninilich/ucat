#!/usr/bin/env bash
# E7 (removed --debug flag), version, --help, and the local part of E4
# (drop without a SQL warehouse). Local only, no UC.
#
# E4 for apply is NOT here: `apply` connects and reads live tables before it
# checks the warehouse (cmd/apply.go: runPipeline, then requireWarehouse), so
# it needs UC and belongs to the live scenarios.
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

new_project flags

# ---------------------------------------------------------------- E7
run plan -t dev --debug
expect_exit "E7 plan --debug" 1
expect_out "E7 plan --debug" 'Error: unknown flag: --debug'
run --debug version
expect_exit "E7 --debug version" 1
expect_out "E7 --debug version" 'Error: unknown flag: --debug'

# ---------------------------------------------------------------- version, help
run version
expect_exit "version" 0
expect_out "version" 'ucat '
expect_out "version" '(commit: '
expect_out "version" ', built: '
expect_not_out "version" 'built: )'
run --help
expect_exit "help" 0
for c in apply drop init plan pull validate version; do
  expect_out "help" "  $c "
done
expect_not_out "help" '--auto-approve'
expect_not_out "help" '--debug'

# ---------------------------------------------------------------- E4 (drop only)
new_project e4
printf 'workspace:\n  host: https://smoke.invalid\n' >targets/dev.yml
run drop -t dev --schema demo_schema --table mt_wh --auto-approve
expect_exit "E4 drop" 1
expect_out "E4 drop" 'Error: drop requires a SQL warehouse: set workspace.warehouse_id in targets/dev.yml (or ucat.yml), or DATABRICKS_WAREHOUSE_ID'
expect_not_out "E4 drop" 'dropped'
# E3: drop does not load tables, so a typo in tables_dir prints no warning.
printf 'project: smoke\ntables_dir: tabels\n' >ucat.yml
run drop -t dev --schema demo_schema --table mt_wh --auto-approve
expect_exit "E3 drop" 1
expect_out "E3 drop" 'drop requires a SQL warehouse'
expect_not_out "E3 drop" 'tables_dir'
# drop without --auto-approve is refused before anything else.
run drop -t dev --schema demo_schema --table mt_wh
expect_exit "drop no approve" 1
expect_out "drop no approve" 'Error: drop requires --auto-approve'

# ---------------------------------------------------------------- broken config files
# drop runs the local validation of catalogs/*.yml and targets/*.yml before it connects (it does not
# load the YAML of tables). The fixture has no warehouse_id, so a drop that passes the validation
# stops at "drop requires a SQL warehouse": that is the positive control of the broken cases.
new_project z62
printf 'workspace:\n  host: https://smoke.invalid\n' >targets/dev.yml
run drop -t dev --schema demo_schema --table mt_never --auto-approve
expect_exit "valid config" 1
expect_out "valid config" 'drop requires a SQL warehouse'
expect_not_out "valid config" 'is not supported'
# a broken catalog file
echo 'foo: 1' >>catalogs/demo_catalog.yml
run drop -t dev --schema demo_schema --table mt_never --auto-approve
expect_exit "broken catalog" 1
expect_out "broken catalog" 'error: field "foo" is not supported; if the file was written for a newer ucat, upgrade ucat (catalogs/demo_catalog.yml:'
expect_out "broken catalog" 'Error: failed to load catalog configs'
expect_not_out "broken catalog" 'requires a SQL warehouse'
expect_not_out "broken catalog" 'dropped'
# a broken target file (the catalog is restored)
printf 'name: demo_catalog\nuc_name: demo_catalog\nschemas:\n  demo_schema:\n    uc_name: demo_schema\n' \
  >catalogs/demo_catalog.yml
echo 'foo: 1' >>targets/dev.yml
run drop -t dev --schema demo_schema --table mt_never --auto-approve
expect_exit "broken target" 1
expect_out "broken target" 'error: field "foo" is not supported; if the file was written for a newer ucat, upgrade ucat (targets/dev.yml:'
expect_out "broken target" 'failed to load target "dev"'
expect_not_out "broken target" 'requires a SQL warehouse'
expect_not_out "broken target" 'dropped'
# a broken YAML of a TABLE does not stop drop (it does not load tables)
printf 'workspace:\n  host: https://smoke.invalid\n' >targets/dev.yml
printf 'foo: 1\n' >tables/demo_schema/bad.yml
run drop -t dev --schema demo_schema --table mt_never --auto-approve
expect_exit "broken table yaml" 1
expect_out "broken table yaml" 'drop requires a SQL warehouse'
expect_not_out "broken table yaml" 'is not supported'

summary
