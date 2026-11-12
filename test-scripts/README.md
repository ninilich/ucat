# test-scripts

Shell-level checks that run the built `ucat` binary the way a user would. They complement the Go unit tests (`make test`).

## smoke/

Offline checks of the CLI surface: `validate`, `init` and the global flags. They need no Databricks workspace, no credentials and no network.

```bash
make test-smoke
```

`smoke/run.sh` builds the binary once, runs `validate.sh`, `init.sh` and `flags.sh` in a temporary directory and prints `SMOKE OK` or `SMOKE FAILED`. Shared helpers are in `smoke/lib.sh`. To add a check, put it in the matching script (or a new script listed in `run.sh`) and use the helpers from `lib.sh`.

These run in CI on every pull request.

## live/

End-to-end scenarios against a real Unity Catalog sandbox: create, change, rename and drop tables, `pull`, interrupts, and so on. They exist to catch behavior that only a real workspace shows (SQL warehouse responses, Unity Catalog defaults, permissions).

The scripts depend on one maintainer's private workspace, so they are **not** in the repository: everything under `live/` except `.gitkeep` is ignored by git. `make test-live` prints an instruction and does nothing unless run with `CONFIRM=yes`.

You do not need `live/` to contribute. The unit tests and `make test-smoke` are what a pull request has to pass. If you want live runs of your own, create `test-scripts/live/run.sh` with scenarios against a catalog and schema you own; the Makefile target will pick it up. See [CONTRIBUTING.md](../CONTRIBUTING.md) for the overall test strategy.
