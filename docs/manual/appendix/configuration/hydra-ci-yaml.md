# Configuration: .hydra-ci.yaml

CI pipeline configuration file.

## Location

```
charts-repository/.hydra-ci.yaml
```

## Structure

```yaml
# Cluster deployment configuration
clusters:
  test:
    auto-deploy: true
    sync-after-deploy: true
  staging:
    auto-deploy: true
    require-approval: false
  prod:
    auto-deploy: false
    require-approval: true

# Test configuration
test:
  parallel: 4
  timeout: 300

# Release configuration
release:
  branch: main
  registry: harbor.example.com/charts
```

## Key Fields

| Field | Description |
|-------|-------------|
| `clusters.<name>.auto-deploy` | Deploy automatically after merge |
| `clusters.<name>.require-approval` | Require manual approval before deploy |
| `clusters.<name>.sync-after-deploy` | Trigger ArgoCD sync after deploy |
| `test.parallel` | Number of parallel test workers |
| `release.branch` | Branch that triggers releases |
| `release.registry` | Chart registry URL |

## See Also

- [Workflow: CI Pipeline](../workflows/ci-pipeline.md)
- [Commands: CI](../commands/ci/)
- [hydra ci config](../commands/ci/config.md)
