.PHONY: build test test-smoke test-live lint snapshot clean

# Version info for local builds; goreleaser (snapshot/release) sets its own via .goreleaser.yml.
COMMIT := $(shell c=$$(git rev-parse --short HEAD 2>/dev/null) && { [ -z "$$(git status --porcelain 2>/dev/null)" ] && echo $$c || echo $$c-dirty; } || echo none)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X github.com/ninilich/ucat/cmd.commit=$(COMMIT) -X github.com/ninilich/ucat/cmd.buildDate=$(BUILD_DATE)

build:
	go build -ldflags "$(LDFLAGS)" -o ucat .

test:
	go test ./...

test-smoke:
	test-scripts/smoke/run.sh

# Live scenarios create and drop tables in a real UC sandbox: print the instruction unless CONFIRM=yes.
test-live:
	@[ -f test-scripts/live/run.sh ] || { echo "test-scripts/live/ is not in the repository: the live scripts are maintained locally against a private sandbox (see CONTRIBUTING.md)."; exit 1; }
ifeq ($(CONFIRM),yes)
	bash test-scripts/live/run.sh
else
	@echo "test-live does NOTHING without CONFIRM=yes."
	@echo "It would run: test-scripts/live/run.sh (the live scripts are local-only and not part of the repository)."
	@echo "The scripts create and DROP tables mt_* in a Unity Catalog sandbox from your local target file;"
	@echo "the project itself is only read."
	@echo "Run:     make test-live CONFIRM=yes"
	@echo "Options: ONLY=stage1_create (one script), KEEP=1 (do not drop the created tables)."
	@echo "Separately (not in the default list): ONLY=stages_scale SCALE_N=64 SCALE_P=\"1 2\" - load test, shared warehouse."
endif

lint:
	golangci-lint run ./...

snapshot:
	goreleaser build --snapshot --clean

clean:
	rm -f ucat
	rm -rf dist/
