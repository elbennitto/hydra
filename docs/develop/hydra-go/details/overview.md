# Architecture Overview

## Overview

Hydra is a CLI tool for managing GitOps deployments with ArgoCD and Helm. The Go backend handles Helm chart rendering, Kubernetes entity processing, dependency analysis, and cluster management. It produces the `.hydra.yaml` dependency graph consumed by the [Hydra UI](../../hydra-ui/).

## Module Structure

The project is split into three Go modules with a strict dependency hierarchy:

```text
hydra-go/
├── go.work               Go workspace for multi-module development
├── test.sh               Build, lint, and test all modules
├── update_testdata.sh    Regenerate golden test files
├── base/                 Generic utilities (no Hydra/Cobra/K8s dependencies)
├── core/                 Business logic (no Cobra dependencies)
└── cli/                  CLI implementation with Cobra
```text

### Dependency Direction

```text
cli ──→ core ──→ base
```

| Module   | May import | Must not import | Purpose                                             |
| -------- | ---------- | --------------- | --------------------------------------------------- |
| **base** | (none)     | core, cli       | Generic utilities, logging, error types             |
| **core** | base       | cli             | Business logic, Helm, Kubernetes, entity processing |
| **cli**  | base, core | —               | Cobra commands, flags, action handlers              |

### Go Modules

Each module is a separate Go module with its own `go.mod`. Local references use `replace` directives:

```go
// core/go.mod
module hydra-gitops.org/hydra/hydra-go/core
require hydra-gitops.org/hydra/hydra-go/base v0.0.0
replace hydra-gitops.org/hydra/hydra-go/base => ../base

// cli/go.mod
module hydra-gitops.org/hydra/hydra-go/cli
require hydra-gitops.org/hydra/hydra-go/base v0.0.0
require hydra-gitops.org/hydra/hydra-go/core v0.0.0
replace hydra-gitops.org/hydra/hydra-go/base => ../base
replace hydra-gitops.org/hydra/hydra-go/core => ../core
```text

The `go.work` file enables workspace-wide development:

```go
go 1.26
use (
    ./base
    ./core
    ./cli
)
```

## Key External Dependencies

| Dependency                       | Version | Module    | Purpose                                |
| -------------------------------- | ------- | --------- | -------------------------------------- |
| `helm.sh/helm/v4`                | v4.1.1  | core      | Helm chart loading, rendering, values  |
| `k8s.io/apimachinery`            | v0.35.1 | core, cli | Kubernetes types, unstructured objects |
| `k8s.io/client-go`               | v0.35.1 | core      | Kubernetes API client                  |
| `k8s.io/cli-runtime`             | v0.35.1 | core, cli | Kubernetes CLI utilities               |
| `k8s.io/apiextensions-apiserver` | v0.35.1 | core      | CRD type definitions                   |
| `github.com/google/cel-go`       | v0.27.0 | core      | CEL expression evaluation              |
| `github.com/mikefarah/yq/v4`     | v4.52.4 | core      | YAML patching and coloring             |
| `github.com/spf13/cobra`         | v1.10.2 | cli       | CLI framework                          |
| `github.com/goccy/go-yaml`       | v1.19.2 | core      | YAML parsing                           |
| `gopkg.in/yaml.v3`               | v3.0.1  | core      | YAML serialization                     |
| `github.com/stretchr/testify`    | v1.11.1 | core, cli | Test assertions                        |

## Package Map

### base/

| Package   | Description                                                  |
| --------- | ------------------------------------------------------------ |
| `cache/`  | Generic thread-safe cache with lazy loading                  |
| `colors/` | ANSI color codes for terminal output                         |
| `errors/` | Error interface with typed ErrorId                           |
| `log/`    | Structured logging (slog-based, ColorHandler, FormatHandler) |
| `types/`  | Generic types (EnumType, YamlString, ValuesMap)              |
| `utils/`  | Generic helpers (Clone, Ptr, EnvWrapper)                     |

→ See [base/README.md](../base/README.md)

### core/

| Package       | Description                                                                              |
| ------------- | ---------------------------------------------------------------------------------------- |
| `cel/`        | CEL expression compilation and evaluation → [cel.md](cel.md)                             |
| `commands/`   | Business commands (render, list, delete, etc.) → [commands.md](commands.md)              |
| `entity/`     | Entity type system and processing → [entity.md](entity.md)                               |
| `helm/`       | Helm chart processing → [helm.md](helm.md)                                               |
| `hydra/`      | Core Hydra types (Context, Cluster, App) → [context.md](../../shared/details/context.md) |
| `k8s/`        | Kubernetes integration (CRDs, diff, validation)                                          |
| `references/` | Reference discovery between entities → [references.md](references.md)                    |
| `sops/`       | SOPS encryption/decryption                                                               |
| `types/`      | Hydra-specific types (Id, GVK, AppId, Config, Value, EntityKey, Ref)                     |
| `values/`     | Values loading and merging → [values.md](values.md)                                      |
| `view/`       | Dependency view and grouping → [grouping.md](../../shared/details/grouping.md)           |
| `yaml/`       | YAML utilities (parsing, printing, stripping server fields)                              |
| `yq/`         | YQ patching and colorization                                                             |

→ See [core/README.md](../core/README.md)

### cli/

| Package   | Description                                                          |
| --------- | -------------------------------------------------------------------- |
| `action/` | CLI action handlers → [cli.md](cli.md)                               |
| `cmd/`    | Cobra command definitions → [cli.md](cli.md)                         |
| `flags/`  | Flag definitions and config creation → [cli.md](cli.md)              |
| `util/`   | Cobra-specific utilities (FlagBuilder, AddPreRun) → [cli.md](cli.md) |

→ See [cli/README.md](../cli/README.md)

## End-to-End Data Flow

The main data flow from Helm chart to dependency graph:

```text
Hydra Context (GitOps directory)
  │
  ▼
hydra.Context / Cluster / RootApp / ChildApp          (hydra/)
  │  Resolves paths, loads values, validates config
  │
  ▼
helm.Template()                                        (helm/)
  │  Renders chart with merged values
  │  Returns rendered manifest with # Source: comments
  │
  ▼
helm.SplitManifestMap()                                (helm/)
  │  Splits manifest by --- separator
  │  Extracts # Source: paths
  │  Returns map[path][]YamlString
  │
  ▼
entity.NewEntitiesFromYaml(l, manifest, key)             (entity/)
  │  Converts YAML documents to Entity objects
  │  Sets templatePath, templateIndex, and unstructured resource under key
  │
  ▼
references.Refs(l, entities, key)                       (references/)
  │  Discovers references between entities via CEL ref-parsers
  │  Resolves incoming/outgoing endpoints
  │
  ▼
view.ToModel(l, entities)                               (view/)
  │  Computes entity groups (seed absorption, union-find)
  │  Builds DependenciesModel
  │
  ▼
view.RenderDependencies(l, writer, entities)            (view/)
  │  Calls ToModel, marshals to YAML, writes to writer
  │
  ▼
.hydra.yaml                                            → consumed by Hydra UI
```text

## Build & Test

### Build

```bash
cd cli && go build -o hydra .
```

### Test

```bash
# Run all tests across all modules
./test.sh
```text

The test script performs for each module (`base`, `core`, `cli`):

1. `go mod tidy` — Clean dependencies
2. `gofmt -w .` — Format code
3. `go vet ./...` — Static analysis
4. `staticcheck ./...` — Extended static analysis
5. `gopls check` — Language server checks
6. `go build ./...` — Compile
7. `gotest -v ./...` — Run tests

### Update Golden Files

```bash
# Regenerate expected files for references and view tests
./update_testdata.sh
```

## Testing Strategy

Tests follow the **golden file pattern**: YAML input → function under test → comparison against `*.expected.yaml`.

| Test location               | Function tested   | Pattern                                            |
| --------------------------- | ----------------- | -------------------------------------------------- |
| `core/references/testdata/` | `Refs()`          | `kubernetes/{apiVersion}/{Kind}/{case}.given.yaml` |
| `core/view/testdata/`       | `computeGroups()` | `kubernetes/{case}.given.yaml`                     |

**Rules:**

- `.expected.yaml` files are **auto-generated** — do NOT add comments
- Add comments explaining test scenarios to `.given.yaml` files
- Use `-update` flag to regenerate golden files: `go test ./... -update`
- Test data is embedded via `//go:embed` directives

## Design Principles

1. **No circular dependencies** — Modules have a strict hierarchy: `cli → core → base`
2. **Framework-agnostic core** — Business logic is independent of CLI framework (Cobra)
3. **Testability** — Core can be tested without CLI framework, no DOM/browser needed
4. **Reusability** — Base utilities can be used in other projects
5. **Golden file tests** — Deterministic, reproducible test data
6. **CEL for extensibility** — Reference parsers and predicates use CEL expressions
