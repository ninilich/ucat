## What and why

## Checklist

- [ ] An issue was discussed first (for anything bigger than a small fix)
- [ ] `go build ./...`, `go vet ./...`, `gofmt -l .`, `make lint`, `go test ./...`, `make test-smoke` pass
- [ ] Tests fail when the code is broken (checked on purpose)
- [ ] No `//nolint`; golden files changed only on purpose (explain the diff)
- [ ] Docs updated if behaviour, flags, YAML keys or JSON fields changed
- [ ] Compatibility kept: in 1.x only optional keys and fields are added
