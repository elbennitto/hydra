# hydra ci run upgrade

CI-driven service version upgrade.

## Synopsis

```bash
hydra ci run upgrade <config-path>
```

## Description

Receives a service version from an external deployment pipeline, updates the matching chart dependency in `dev/`, validates the result, and then pushes directly or opens an MR when rendered test data also changed.

## Examples

```bash
hydra ci run upgrade .hydra-ci.yaml
hydra ci run upgrade --dry-run .hydra-ci.yaml
```

## See Also

- [hydra gitops apply](../cluster/apply.md) — Interactive apply
- [Workflow: CI Pipeline](../../appendix/workflows/ci-pipeline.md)
