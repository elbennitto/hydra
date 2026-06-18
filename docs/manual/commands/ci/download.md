# hydra ci run download

Download chart dependencies for changed charts.

## Synopsis

```bash
hydra ci run download <config-path> [flags]
```

## Description

Runs chart-level dependency resolution for changed charts in the configured environments:
1. Detects changed charts
2. Runs `helm dependency update` for each changed chart

This step refreshes dependencies even when artifacts already exist under `charts/`.
Use it before `hydra ci run test` when the local chart dependency cache needs to be rebuilt.

## Examples

```bash
hydra ci run download .hydra-ci.yaml
hydra ci run download .hydra-ci.yaml --dry-run
```

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Dependencies downloaded successfully |
| 1 | Download or validation failure |

## See Also

- [hydra ci run test](test.md) — validate changed charts using already-downloaded dependencies
- [Workflow: CI Pipeline](../../appendix/workflows/ci-pipeline.md)
