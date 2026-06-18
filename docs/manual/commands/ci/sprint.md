# hydra ci run sprint

Sprint management automation.

## Synopsis

```bash
hydra ci run sprint create
hydra ci run sprint close
```

## Description

Automates sprint lifecycle:
- **`sprint create`** — Creates a new sprint branch and sets up version references
- **`sprint close`** — Closes the current sprint, merges back to main

## Examples

```bash
hydra ci run sprint create
hydra ci run sprint close
```

## See Also

- [Workflow: Sprint Management](../../workflows/sprint-management.md)
- [Workflow: CI Pipeline](../../appendix/workflows/ci-pipeline.md)
