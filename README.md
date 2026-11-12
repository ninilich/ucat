<p align="center">
  <a href="https://ninilich.github.io/ucat/">
    <img src="docs/assets/ucat-header.png" width="280" alt="ucat">
  </a>
</p>

<p align="center">
  <strong>Unity Catalog tables, changed like code.</strong><br>
  Declare. Plan. Apply.
</p>

<p align="center">
  <a href="https://ninilich.github.io/ucat/">Website</a> ·
  <a href="docs/getting-started.md">Getting started</a> ·
  <a href="docs/README.md">Documentation</a> ·
  <a href="ROADMAP.md">Roadmap</a> ·
  <a href="https://github.com/ninilich/ucat/actions/workflows/ci.yml"><img src="https://github.com/ninilich/ucat/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
</p>

**ucat** keeps your Databricks managed table definitions in git as YAML, compares them with what is really in Unity Catalog,
shows exactly what would change, and applies it from CI. The same definitions go to dev, test and prod, one target at a
time. No state file.

```
$ ucat plan -t dev
`dev`.`silver`.`orders`
  ~ set 1 tblproperties
  ~ add column "discount" DOUBLE

Plan: 0 to add, 1 to change, 0 to destroy.

$ ucat apply -t dev
[1/2] applied in 0.9s: set 1 tblproperties
[2/2] applied in 1.4s: add column "discount" DOUBLE
Apply complete in 2.3s.
```

Why **ucat**, how it works, and how it compares with Terraform, Bundles, dbt and notebooks:
[the website](https://ninilich.github.io/ucat/).

## Quickstart

```bash
# 1. Log in to your workspace (needs the Databricks CLI; in CI use environment variables, see docs/authentication.md)
databricks auth login --host https://<workspace>.cloud.databricks.com

# 2. Create a project (asks for the workspace host, the UC catalog and the SQL warehouse)
ucat init

# 3. Import the tables you already have
ucat pull -t dev

# 4. Change a YAML file, then see what would change
ucat plan -t dev

# 5. Apply
ucat apply -t dev
```

More: [Getting started](docs/getting-started.md), [the quickstart example](examples/quickstart/README.md).

## Install

Not released yet. The first release will ship binaries for Linux, macOS and Windows on GitHub Releases. For now, from
source (Go 1.26 or newer):

```bash
go install github.com/ninilich/ucat@latest
```

## Documentation

All documents are listed in [docs/README.md](docs/README.md). v1 manages managed tables only; see
[Limitations](docs/limitations.md) for what it does not do and the [Roadmap](ROADMAP.md) for what comes next.

## Contributing

**ucat** is open source and built in the open. Issues, ideas and pull requests are welcome, from a typo fix to a new
feature. Not sure where to start? Look for issues labelled `good first issue`, or tell us how you use Unity Catalog and
what is missing. See [CONTRIBUTING.md](CONTRIBUTING.md) to build and test, and
[the architecture](docs/contributing/architecture.md) to find your way in the code. Everyone taking part follows the
[Code of Conduct](CODE_OF_CONDUCT.md).

## License

Apache License 2.0. You may use **ucat** at work, modify it and build it into your own tools, keeping the license and
copyright notices. This is general information, not legal advice. Third-party licenses:
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
