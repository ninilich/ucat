#!/usr/bin/env bash
# Common helpers for the local smoke scripts (no network, no Databricks).
# Source this file; do not run it. Each script calls `summary` at the end.

set -u

SMOKE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SMOKE_DIR/../.." && pwd)"

PASS_COUNT=0
FAIL_COUNT=0
OUT=""      # stdout of the last run
ERR=""      # stderr of the last run
RC=0        # exit code of the last run

# Temporary directory: created once per run.sh (inherited via UCAT_SMOKE_TMP),
# or per script when started alone. Only the creator removes it.
if [ -z "${UCAT_SMOKE_TMP:-}" ]; then
  UCAT_SMOKE_TMP="$(mktemp -d)"
  export UCAT_SMOKE_TMP
  UCAT_SMOKE_OWNER=1
  trap 'rm -rf "$UCAT_SMOKE_TMP"' EXIT
  trap 'exit 130' INT TERM
fi
WORK="$UCAT_SMOKE_TMP/work"
mkdir -p "$WORK" "$UCAT_SMOKE_TMP/home"

# Build the binary once per run.
build_ucat() {
  if [ -n "${UCAT_BIN:-}" ] && [ -x "$UCAT_BIN" ]; then
    return 0
  fi
  local exe="ucat"
  case "$(uname -s)" in MINGW* | MSYS* | CYGWIN*) exe="ucat.exe" ;; esac
  UCAT_BIN="$UCAT_SMOKE_TMP/$exe"
  if ! (cd "$REPO_ROOT" && go build -o "$UCAT_BIN" .); then
    echo "FAIL build: go build failed" >&2
    exit 1
  fi
  export UCAT_BIN
}

# new_project DIR: minimal project fixture (one schema demo_schema, catalog
# demo_catalog, target dev with a dummy warehouse). DIR becomes the cwd.
new_project() {
  local dir="$WORK/$1"
  rm -rf "$dir"
  mkdir -p "$dir/catalogs" "$dir/targets" "$dir/tables/demo_schema"
  printf 'project: smoke\ntables_dir: tables\n' >"$dir/ucat.yml"
  printf 'name: demo_catalog\nuc_name: demo_catalog\nschemas:\n  demo_schema:\n    uc_name: demo_schema\n' \
    >"$dir/catalogs/demo_catalog.yml"
  printf 'workspace:\n  host: https://smoke.invalid\n  warehouse_id: smoke-dummy-wh\n' >"$dir/targets/dev.yml"
  cd "$dir" || exit 1
}

# run ARGS...: run ucat in the cwd with stdin from /dev/null; sets OUT, ERR, RC.
# The environment is isolated: empty HOME, no DATABRICKS_* variables.
run() { run_stdin "" "$@"; }

# run_stdin TEXT ARGS...: same, with TEXT (printf format applied to nothing) on stdin.
run_stdin() {
  local input="$1"
  shift
  build_ucat
  local o="$UCAT_SMOKE_TMP/stdout" e="$UCAT_SMOKE_TMP/stderr" i="$UCAT_SMOKE_TMP/stdin"
  printf '%s' "$input" >"$i"
  # Unset every Databricks/Azure auth variable the SDK could pick up, not just known ones.
  local unset_args=() v
  while IFS= read -r v; do unset_args+=(-u "$v"); done < <(env | grep -oE '^(DATABRICKS|ARM|AZURE)_[A-Za-z0-9_]*' || true)
  env ${unset_args[@]+"${unset_args[@]}"} \
    HOME="$UCAT_SMOKE_TMP/home" "$UCAT_BIN" "$@" <"$i" >"$o" 2>"$e"
  RC=$?
  OUT="$(cat "$o")"
  ERR="$(cat "$e")"
}

# Short fragment of the last output for FAIL lines.
_snippet() {
  printf '%s\n%s' "$OUT" "$ERR" | tr '\n' ' ' | cut -c1-200
}

_pass() {
  PASS_COUNT=$((PASS_COUNT + 1))
  echo "PASS $1: $2"
}

_fail() {
  FAIL_COUNT=$((FAIL_COUNT + 1))
  echo "FAIL $1: $2 (got rc=$RC, output: $(_snippet))"
}

# expect_exit SCENARIO WANT_CODE
expect_exit() {
  if [ "$RC" -eq "$2" ]; then _pass "$1" "exit code $2"; else _fail "$1" "exit code $2"; fi
}

# expect_nonzero SCENARIO
expect_nonzero() {
  if [ "$RC" -ne 0 ]; then _pass "$1" "exit code != 0 ($RC)"; else _fail "$1" "exit code != 0"; fi
}

# expect_out SCENARIO SUBSTRING: substring present in stdout+stderr.
expect_out() {
  if printf '%s\n%s\n' "$OUT" "$ERR" | grep -qF -- "$2"; then
    _pass "$1" "output contains: $2"
  else
    _fail "$1" "output contains: $2"
  fi
}

# expect_not_out SCENARIO SUBSTRING
expect_not_out() {
  if printf '%s\n%s\n' "$OUT" "$ERR" | grep -qF -- "$2"; then
    _fail "$1" "output does not contain: $2"
  else
    _pass "$1" "output does not contain: $2"
  fi
}

# expect_count SCENARIO SUBSTRING N: substring appears on exactly N lines.
expect_count() {
  local n
  n="$(printf '%s\n%s\n' "$OUT" "$ERR" | grep -cF -- "$2")"
  if [ "$n" -eq "$3" ]; then
    _pass "$1" "output has $3 line(s) with: $2"
  else
    _fail "$1" "output has $3 line(s) with: $2 (found $n)"
  fi
}

# expect_empty_dir SCENARIO DIR: no files in DIR (ls -A empty).
expect_empty_dir() {
  if [ -z "$(ls -A "$2")" ]; then _pass "$1" "directory is empty"; else _fail "$1" "directory is empty (has: $(ls -A "$2" | tr '\n' ' '))"; fi
}

# expect_file SCENARIO PATH
expect_file() {
  if [ -f "$2" ]; then _pass "$1" "file exists: $2"; else _fail "$1" "file exists: $2"; fi
}

# expect_file_has SCENARIO PATH SUBSTRING: file content contains SUBSTRING.
expect_file_has() {
  if grep -qF -- "$3" "$2" 2>/dev/null; then _pass "$1" "$2 contains: $3"; else _fail "$1" "$2 contains: $3"; fi
}

summary() {
  echo "$PASS_COUNT passed, $FAIL_COUNT failed"
  [ "$FAIL_COUNT" -eq 0 ]
}
