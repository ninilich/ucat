#!/usr/bin/env bash
# Run all local smoke scripts; exit non-zero if any check fails.
# One shared temp dir and one binary build for the whole run.
set -u
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

UCAT_SMOKE_TMP="$(mktemp -d)"
export UCAT_SMOKE_TMP
trap 'rm -rf "$UCAT_SMOKE_TMP"' EXIT
trap 'exit 130' INT TERM

# shellcheck source=lib.sh
. "$HERE/lib.sh"
build_ucat

failed=0
for s in validate init flags; do
  echo "== $s.sh"
  bash "$HERE/$s.sh" || failed=1
done

if [ "$failed" -eq 0 ]; then
  echo "SMOKE OK"
else
  echo "SMOKE FAILED"
fi
exit "$failed"
