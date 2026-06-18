# hydra ci run promote

Promote a release to the next stage.

## Synopsis

```bash
hydra ci run promote <version> <target-stage>
```

## Description

Moves a release from its current stage to the next (e.g., test → staging → production). Updates the GitOps repository to reference the promoted version.

## Examples

```bash
hydra ci run promote v2.5.0 staging
hydra ci run promote v2.5.0 production
```

## See Also

- [hydra ci run release](release.md)
- [Workflow: Release Process](../../workflows/release-process.md)
