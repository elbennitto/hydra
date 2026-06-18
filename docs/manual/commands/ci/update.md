# hydra ci run update

Update dependencies and versions.

## Synopsis

```bash
hydra ci run update [flags]
```

## Description

Automatically updates version references in the charts-repository:
- Helm chart dependencies
- Docker image tags
- Inter-app version references

Used in CI to create automated update merge requests.

## Examples

```bash
hydra ci run update
```

## See Also

- [Workflow: CI Pipeline](../../appendix/workflows/ci-pipeline.md)
- [Workflow: Version Updates](../../workflows/version-updates.md)
