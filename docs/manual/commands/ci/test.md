# hydra ci run test

Run CI validation tests.

## Synopsis

```bash
hydra ci run test [flags]
```

## Description

Runs chart-level CI validation for changed charts in the configured environments:
1. Verifies that all chart dependencies are already available locally
2. Runs `helm lint`
3. Runs `helm template`

Typically called in the CI pipeline on every merge request.

`hydra ci run test` does not download dependencies. If a dependency is missing, the command fails and tells you to run `hydra ci run download` first.

If a chart is not meant to render standalone with its normal `values.yaml`, you can place an optional `values.test.yaml` next to it in the chart directory. `hydra ci run test` automatically passes that file to `helm lint` and `helm template` when present.

Example:

```yaml
# charts-repository/apps/demo/service-auth/dev/values.test.yaml
global:
	baseUrl: https://dummy.invalid
```

## Flags

| Flag | Description |
|------|-------------|
| `--parallel <n>` | Number of parallel workers |

## Examples

```bash
hydra ci run test
hydra ci run test --parallel 4
```

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | All tests pass |
| 1 | Validation failure |

## See Also

- [hydra ci run download](download.md) — fetch dependencies for changed charts
- [hydra local test refs](../local/test.md) — The local equivalent
- [Workflow: CI Pipeline](../../appendix/workflows/ci-pipeline.md)
