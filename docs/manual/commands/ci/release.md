# hydra ci run release

Create a versioned release.

## Synopsis

```bash
hydra ci run release [flags]
```

## Description

Creates a new release version by:
1. Determining the next version number
2. Tagging the charts-repository
3. Building release artifacts
4. Publishing release metadata

## Flags

| Flag | Description |
|------|-------------|
| `--bump <major\|minor\|patch>` | Version bump level |
| `--dry-run` | Preview without making changes |

## Examples

```bash
hydra ci run release --bump minor
hydra ci run release --bump patch --dry-run
```

## See Also

- [hydra ci run promote](promote.md)
- [hydra ci run publish](publish.md)
- [Workflow: Release Process](../../workflows/release-process.md)
