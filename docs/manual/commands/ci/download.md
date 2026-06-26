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

For private OCI dependencies, Hydra reads registry credentials from
`.hydra-ci-secrets.sops.yaml` via `secrets.registryTokens`. If a registry
returns `401` or Helm reports `basic credential not found` and no matching
token is configured, Hydra extends the error with a concrete secret example.

Example CI secrets snippet:

```yaml
secrets:
  registryTokens:
    - registry: harbor.example.test
      username: robot$hydra-read
      token: <read-token>
```

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
