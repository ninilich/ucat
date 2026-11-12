#!/usr/bin/env bash
# E5: ucat init - empty target list and duplicate target names (local, no UC).
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

INIT_HEAD='proj\nmain\nsilver\nsilver\n'

# init_case LABEL TARGETS_LINE EXPECTED_ERROR
init_case() {
  local d="$WORK/init_$1"
  rm -rf "$d"
  mkdir -p "$d"
  cd "$d" || exit 1
  # shellcheck disable=SC2059
  run_stdin "$(printf "${INIT_HEAD}%s\n" "$2")" init
  expect_exit "E5 $1" 1
  expect_out "E5 $1" "$3"
  expect_not_out "E5 $1" "Workspace host"
  expect_empty_dir "E5 $1" "$d"
}

init_case "empty list ','" ',' 'Error: target names: at least one name is required'
init_case "dev,dev" 'dev,dev' 'Error: duplicate target name "dev"'
init_case "dev,prod,Dev" 'dev,prod,Dev' 'Error: duplicate target name "Dev"'
init_case "spaces ' dev , dev '" ' dev , dev ' 'Error: duplicate target name "dev"'

# Happy path: two distinct targets.
d="$WORK/init_ok"
rm -rf "$d"
mkdir -p "$d"
cd "$d" || exit 1
# shellcheck disable=SC2059
run_stdin "$(printf "${INIT_HEAD}dev,prod\nhttps://h1\ndev_cat\nw1\nhttps://h2\nprod_cat\nw2\n")" init
expect_exit "E5 dev,prod" 0
expect_file "E5 dev,prod" targets/dev.yml
expect_file "E5 dev,prod" targets/prod.yml
expect_file "E5 dev,prod" ucat.yml
expect_file "E5 dev,prod" catalogs/main.yml
expect_file_has "E5 dev,prod" targets/dev.yml 'https://h1'
expect_file_has "E5 dev,prod" targets/dev.yml 'w1'
expect_file_has "E5 dev,prod" targets/prod.yml 'https://h2'
expect_file_has "E5 dev,prod" targets/prod.yml 'w2'
expect_file_has "E5 dev,prod" ucat.yml 'proj'
expect_out "E5 dev,prod" 'ucat pull -t dev'

summary
