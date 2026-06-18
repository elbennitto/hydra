# Hydra Go Architecture Documentation

Hydra Go is the CLI backend for managing GitOps deployments with ArgoCD and Helm. It handles Helm chart rendering, Kubernetes entity processing, dependency analysis, and cluster management.

## Topics

| Topic                               | Description                                                                       |
| ----------------------------------- | --------------------------------------------------------------------------------- |
| [Overview](overview.md)             | Module structure, dependency hierarchy, key types, build & test                   |
| [CEL](cel.md)                       | CEL expression environment, predicates, ref-builder, custom functions             |
| [CLI](cli.md)                       | Cobra command hierarchy, action handlers, flag system, dependency injection       |
| [Commands](commands.md)             | Business commands: render, list, apply, uninstall, scale, delete, bootstrap       |
| [Diff](diff.md)                     | Cluster diff with server-side apply dry-run, orphan detection, YAML comparison    |
| [Entity](entity.md)                 | Entity type system, key-value model, collections, comparison, selection, grouping |
| [Git](git.md)                       | Git abstraction for CI pipelines: repo, commits, tags, branches, chart builder    |
| [Helm](helm.md)                     | Chart loading, dependency management, values processing, template rendering       |
| [Pipeline](pipeline.md)             | CI/CD pipelines: test, release, promote, publish, sprint, upgrade, sync, update    |
| [Pipeline Tests](pipeline-tests.md) | Test case specifications for all CI pipelines, golden file patterns               |
| [References](references.md)         | Reference discovery via CEL ref-parsers, provider endpoints, dependency ordering  |
| [Values](values.md)                 | Values hierarchy, deep merge, Helm values processing, HydraValues extraction      |
| [Log progress](log-progress.md)     | ProgressBars, Progress, mpb in cli/progress; slog output; NewProgress and Close   |

Each topic file contains a summary. For full implementation details, see the `details/` subdirectory.
