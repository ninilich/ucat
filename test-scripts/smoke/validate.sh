#!/usr/bin/env bash
# Part A (all validation diagnostics at once), E1 (YAML syntax errors),
# E3 (tables_dir warnings), E8 (absolute tables_dir). Local only, no UC.
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

T=tables/demo_schema

# ---------------------------------------------------------------- Part A
new_project part_a
printf 'name: BadTable\nschema: demo_schema\ncolumns:\n  - name: id\n    type: TEXT\n  - name: Id\n    type: BIGINT\n  - name: "a.b"\n    type: INTEGER\n' >"$T/bad.yml"
printf 'name: a\nschema: demo_schema\nrename_from: b\ncolumns:\n  - name: id\n    type: BIGINT\n' >"$T/a.yml"
printf 'name: b\nschema: demo_schema\nrename_from: a\ncolumns:\n  - name: id\n    type: BIGINT\n' >"$T/b.yml"
run validate
expect_exit "A validate" 1
expect_out "A name case" 'table name "BadTable" must be lowercase'
expect_out "A type TEXT" 'column "id": type "TEXT" is not supported; supported types:'
expect_out "A type INTEGER" 'column "a.b": type "INTEGER" is not supported; supported types:'
expect_out "A duplicate column" 'duplicate column "Id" (column names are case-insensitive)'
expect_out "A dot in name" 'column "a.b": name contains a dot or whitespace'
expect_out "A rename a" 'table "a": rename_from "b" refers to a table that is also defined in'
expect_out "A rename b" 'table "b": rename_from "a" refers to a table that is also defined in'
expect_out "A summary" 'Error: local validation failed'

# Errors in the catalog config do not hide the table errors (validation never stops at the first).
new_project part_a2
printf 'name: demo_catalog\nuc_name: Bad_Catalog\nschemas:\n  demo_schema:\n    uc_name: Bad_Schema\n' >catalogs/demo_catalog.yml
printf 'name: BadTable\nschema: demo_schema\ncolumns:\n  - name: id\n    type: TEXT\n' >"$T/bad.yml"
run validate
expect_exit "A2 validate" 1
expect_out "A2 catalog case" 'catalog "Bad_Catalog" must be lowercase'
expect_out "A2 schema case" 'schema "Bad_Schema" must be lowercase'
expect_out "A2 table case" 'table name "BadTable" must be lowercase'
expect_out "A2 type TEXT" 'column "id": type "TEXT" is not supported'
expect_out "A2 summary" 'Error: local validation failed'

# ---------------------------------------------------------------- E1
new_project e1
printf 'name: bad_syntax\nschema: demo_schema\ncolumns:\n  - name: id\n    type: [BIGINT\n' >"$T/bad_syntax.yml"
for cmd in validate "plan -t dev"; do
  # shellcheck disable=SC2086
  run $cmd
  expect_exit "E1 $cmd" 1
  expect_out "E1 $cmd" 'error: failed to parse: '
  expect_out "E1 $cmd" "error: failed to parse: did not find expected ',' or ']' ($T/bad_syntax.yml:4)"
  expect_out "E1 $cmd" 'Error: failed to load tables'
done
# CRLF gives the same text.
printf 'name: bad_syntax\r\nschema: demo_schema\r\ncolumns:\r\n  - name: id\r\n    type: [BIGINT\r\n' >"$T/bad_syntax.yml"
run validate
expect_exit "E1 CRLF" 1
expect_out "E1 CRLF" "error: failed to parse: did not find expected ',' or ']' ($T/bad_syntax.yml:4)"
expect_not_out "E1 CRLF" $'\r'
expect_out "E1 CRLF" 'Error: failed to load tables'
rm "$T/bad_syntax.yml"
# Type error (unknown field) also carries file and line; a syntax error without a line names the file.
printf 'name: t\nschema: demo_schema\nfoo: 1\ncolumns:\n  - name: id\n    type: BIGINT\n' >"$T/t.yml"
run validate
expect_exit "E1 unknown field" 1
expect_out "E1 unknown field" "error: field \"foo\" is not supported; if the file was written for a newer ucat, upgrade ucat ($T/t.yml:3)"
expect_out "E1 unknown field" 'Error: failed to load tables'
printf 'name: t\x01\n' >"$T/t.yml"
run validate
expect_exit "E1 control char" 1
expect_out "E1 control char" "error: failed to parse $T/t.yml: yaml: control characters are not allowed ($T/t.yml)"
rm "$T/t.yml"
# Broken ucat.yml, catalog and target.
for f in ucat.yml catalogs/demo_catalog.yml targets/dev.yml; do
  new_project e1_cfg
  printf 'a: [b\n' >"$f"
  case "$f" in
    ucat.yml) want='Error: failed to load project config' ;;
    catalogs/*) want='Error: failed to load catalog configs' ;;
    *) want='Error: failed to load target "dev"' ;;
  esac
  run plan -t dev
  expect_exit "E1 $f" 1
  expect_out "E1 $f" "error: failed to parse: did not find expected ',' or ']' ($f:1)"
  expect_out "E1 $f" "$want"
done

# ---------------------------------------------------------------- E3
# Only validate: plan/pull would connect to the (dummy) workspace.
new_project e3
printf 'project: smoke\ntables_dir: tabels\n' >ucat.yml
run validate
expect_exit "E3 typo in tables_dir" 0
expect_out "E3 typo in tables_dir" '✓ all local checks passed'
expect_out "E3 typo in tables_dir" 'warning: tables_dir "tabels" not found (no tables are defined)'
expect_count "E3 typo in tables_dir" 'tables_dir "tabels" not found' 1
# plan prints the same warning once; pull (starts from an empty project) prints none.
# The dummy target has no host, so both stop locally: plan has nothing to compare, pull
# fails when it first asks the backend (no network involved).
printf 'workspace:\n  warehouse_id: smoke-dummy-wh\n' >targets/dev.yml
run plan -t dev
expect_exit "E3 plan typo" 0
expect_count "E3 plan typo" 'tables_dir "tabels" not found' 1
expect_out "E3 plan typo" 'No changes.'
run pull -t dev
expect_exit "E3 pull typo" 1
expect_out "E3 pull typo" 'Error: list tables in '
expect_not_out "E3 pull typo" 'tables_dir'
mkdir empty_tables
printf 'project: smoke\ntables_dir: empty_tables\n' >ucat.yml
touch empty_tables/a.yaml
run validate
expect_exit "E3 .yaml file" 0
expect_out "E3 .yaml file" 'warning: no table files (*.yml) found in "empty_tables"; found 1 file(s) named *.yaml or with an upper-case extension (*.YML), which are ignored: rename them to .yml'

# ---------------------------------------------------------------- E8
new_project e8
for v in '/abs/tables' '\rooted' 'C:\x' 'C:/x' 'c:x' '\\server\share\t'; do
  printf 'project: smoke\ntables_dir: '"'%s'"'\n' "$v" >ucat.yml
  for cmd in validate "plan -t dev" "pull -t dev" "drop -t dev --schema demo_schema --table x --auto-approve"; do
    # shellcheck disable=SC2086
    run $cmd
    expect_exit "E8 $v / $cmd" 1
    expect_out "E8 $v / $cmd" "error: tables_dir must be relative to the project root (got \"$v\") (ucat.yml:"
    expect_out "E8 $v / $cmd" 'Error: failed to load project config'
    expect_not_out "E8 $v / $cmd" 'not found'
  done
done
# Relative values are accepted (nonexistent dir is only a warning).
for v in 'tables' './tables' 'tables\prod' '..\tables'; do
  printf 'project: smoke\ntables_dir: '"'%s'"'\n' "$v" >ucat.yml
  run validate
  expect_exit "E8 relative $v" 0
  expect_out "E8 relative $v" '✓ all local checks passed'
  expect_not_out "E8 relative $v" 'must be relative'
  expect_not_out "E8 relative $v" 'failed to load project config'
done
# Backslashes are normalized to "/" (the warning shows the stored spelling).
printf 'project: smoke\ntables_dir: '"'tables\\prod'"'\n' >ucat.yml
run validate
expect_out "E8 relative normalized" 'warning: tables_dir "tables/prod" not found'


summary
