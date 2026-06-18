# hydra ci run auto

Automatic CI decision-making.

## Synopsis

```bash
hydra ci run auto [flags]
```

## Description

Makes automated decisions in CI pipelines:
- Determines which apps were affected by a change
- Decides whether to run tests, upgrade, or skip
- Handles pipeline orchestration logic

## See Also

- [Configuration: .hydra-ci.yaml](../../appendix/configuration/hydra-ci-yaml.md)
- [Workflow: CI Pipeline](../../appendix/workflows/ci-pipeline.md)
