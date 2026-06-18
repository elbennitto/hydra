# Architecture Overview

Hydra is a CLI tool for managing GitOps deployments with ArgoCD and Helm. The Go backend handles Helm chart rendering, Kubernetes entity processing, dependency analysis, and cluster management. It produces the `.hydra.yaml` dependency graph consumed by Hydra UI.

## Key Concepts

- **Three Go modules** — `base` (generic utilities), `core` (business logic), `cli` (Cobra commands) with strict dependency direction: `cli → core → base`
- **Entity system** — Kubernetes resources as typed key-value maps for processing, reference discovery, and grouping
- **Rendering pipeline** — Helm chart → YAML manifest → entities → references → dependency graph → `.hydra.yaml`
- **Golden file testing** — YAML input → function under test → comparison against `*.expected.yaml`
- **CEL expressions** — Used for reference parsers and entity predicates

## Source Files

`base/`, `core/`, `cli/` — each a separate Go module with its own `go.mod`

→ **Full details:** [details/overview.md](details/overview.md)
