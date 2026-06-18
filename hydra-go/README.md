# Hydra Go

The CLI component of [Hydra](../README.md). A command-line tool for debugging and inspecting the Hydra directory structure — both against the local filesystem (rendering templates, merging values, discovering dependencies offline) and against a live Kubernetes cluster (diffing, dumping, validating, uninstalling).

## Architecture

The project is split into three Go modules with a strict dependency hierarchy:

```tree
hydra-go/
    .goreleaser.yml       GoReleaser config for CLI release artifacts
    go.work               Go workspace for multi-module development
    test.sh               Build, lint, and test all modules
    update_testdata.sh    Regenerate golden test files
    base/                 Generic utilities (no Hydra/Cobra/K8s dependencies)
    core/                 Business logic (no Cobra dependencies)
    cli/                  CLI implementation with Cobra
```

### Dependency Direction

```text
cli → core → base
```

| Module | May import | Purpose |
| ------ | ---------- | ------- |
| **base** | — | Generic utilities, logging, caching, error types |
| **core** | base | Business logic, Helm, Kubernetes, entity processing, CEL |
| **cli** | base, core | Cobra commands, flags, action handlers |

### Modules

**base** — Generic utilities without domain-specific logic:
`cache/` `colors/` `errors/` `log/` `types/` `utils/`

**core** — Hydra business logic:
`cel/` `commands/` `entity/` `export/` `git/` `helm/` `hydra/` `k8s/` `references/` `sops/` `types/` `values/` `view/` `yaml/` `yq/`

**cli** — CLI implementation with Cobra:
`action/` `cmd/` `flags/` `util/`

Detailed documentation is in [`architecture/`](architecture/), starting with the [Overview](architecture/OVERVIEW.md).

## Development

### Prerequisites

- [Go](https://go.dev/) 1.26+

### Build

```bash
cd hydra-go
go work sync
cd cli && go build -o hydra .
```

### Release Archives

Run GoReleaser from `hydra-go/`. The config writes artifacts to the repository-root `dist/` directory:

```bash
goreleaser release --clean --snapshot --config .goreleaser.yml
```

### Test

```bash
# All modules (build, lint, vet, test)
./test.sh

# Individual modules
cd base && go test ./...
cd core && go test ./...
cd cli && go test ./...
```

### Update Golden Files

```bash
./update_testdata.sh
```

## Design Principles

1. **No circular dependencies** — Modules have a strict hierarchy
2. **Framework-agnostic core** — Business logic is independent of the CLI framework
3. **Testability** — Core can be tested without CLI framework
4. **Reusability** — Base utilities can be used in other projects
5. **Golden file tests** — Deterministic, reproducible test data
6. **CEL for extensibility** — Reference parsers and predicates use CEL expressions
