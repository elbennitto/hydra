# hydra ci run upgrade

Update service dependency versions from a versions input.

## Synopsis

```bash
hydra ci run upgrade --versions-file <versions-file> <config-path>
hydra ci run upgrade <config-path> < <versions-file>
```

## Description

Reads desired service versions from a YAML file, or from standard input when `--versions-file` is omitted, and updates the matching `Chart.yaml` dependency in each child chart:

```text
<ci.rootAppsPath>/<rootApp>/<app>/<env>/Chart.yaml
```

The versions file has this format:

```yaml
versions:
  - rootApp: demo
    app: service-ui
    env: dev
    version: 1.2.3
```

For each entry, Hydra finds the dependency whose `name` matches `app` and sets its `version` to the requested value. The child chart's own top-level `version` is not changed by `upgrade`; the regular `hydra ci run release` step derives wrapper and root-app version changes from the updated dependency.

Use `--dry-run` to validate the file and log the planned dependency changes without writing `Chart.yaml`.

Use `--skip-missing` to warn and continue when an entry has no matching child chart or dependency. Other validation errors still stop the command.

`upgrade` is for desired service version changes. `hydra ci run update` keeps its separate meaning: rendering clusters and refreshing generated unit test data.

## Flags

| Flag | Description |
| ---- | ----------- |
| `--skip-missing` | Warn and continue when an entry has no matching child chart or dependency |
| `--versions-file` | YAML file containing `versions` entries to apply; when omitted, Hydra reads the same YAML format from stdin |

## Examples

```bash
hydra ci run upgrade --versions-file versions.yaml .hydra-ci.yaml
cat versions.yaml | hydra ci run upgrade .hydra-ci.yaml
hydra ci run upgrade --dry-run --versions-file versions.yaml .hydra-ci.yaml
hydra ci run upgrade --skip-missing --versions-file versions.yaml .hydra-ci.yaml
```

## See Also

- [hydra ci run release](release.md) — Derive wrapper and root-app versions after dependency changes
- [hydra ci run update](update.md) — Refresh rendered unit test data
- [Workflow: CI Pipeline](../../appendix/workflows/ci-pipeline.md)
