# hydra ci run release

Detect changed charts, bump versions, and create release tags.

## Synopsis

```bash
hydra ci run release <config-path>
```

## Description

`hydra ci run release` detects changed child chart directories (build-tag based),
updates child wrapper versions, updates matching root chart version pins, and
runs mode-specific actions.

In `--local` mode, Hydra writes chart files, creates one commit, and adds
lightweight tags:

- `<group>-<app>-<version>`
- `<group>-root-<version>`
- `build-<UTCYYYYMMDDHHmm>`

In `--dry-run` mode, Hydra only logs the planned changes.

In default CI mode (without `--dry-run` / `--local`), Hydra applies the release,
creates tags, and pushes the release commit plus tags to `origin`.

## Flags

| Flag | Description |
|------|-------------|
| `--dry-run` | Simulate the release plan without writing files |
| `--local` | Apply release changes locally (commit + tags), no remote operations |
| `--target-branch <name>` | For local runs, commit on an existing target branch |

## Examples

```bash
hydra ci run release .hydra-ci.yaml --dry-run
hydra ci run release .hydra-ci.yaml --local
hydra ci run release .hydra-ci.yaml --local --target-branch release/work
```

## See Also

- [hydra ci run promote](promote.md)
- [hydra ci run publish](publish.md)
- [hydra ci](README.md)
