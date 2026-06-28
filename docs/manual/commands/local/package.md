# hydra local package

Package one application chart into a Helm `.tgz` archive using the same packaging path as `hydra ci run publish`.

## Synopsis

```text
hydra local package <appId> [flags]
```

## Description

`hydra local package` resolves one Hydra app id to its chart directory, refreshes chart dependencies, applies Hydra's packaging-time chart mutations, and writes a standard Helm chart archive.

The command is local-only and does not push anything to an OCI registry. It is useful when you want to inspect or hand off the exact packaged chart that Hydra CI would build, without running the full CI publish flow.

Like `hydra ci run publish`, packaging supports **`global.hydra.templateFiles`** from the chart `values.yaml`. Hydra applies these file mutations before saving the chart archive. This lets you move a local `templates/...` file onto another loaded chart path, or delete matching template files by regex, before the `.tgz` is created. See [`templateFiles in Values`](../../appendix/values/template-files.md).

## Arguments

| Argument | Description |
| --- | --- |
| `appId` | One [App ID](../README.md#app-ids) selecting a root app or child app |

## Flags

| Flag | Short | Description |
| --- | --- | --- |
| `--hydra-context` | | Path to the [Hydra context directory](../README.md#hydra-context) (or set `HYDRA_CONTEXT` env var) |
| `--helm-network-mode` | | [Helm network mode](../README.md#helm-network-mode): `online`, `local`, `offline`, or `error` |
| `--no-cache` | | Disable persistent Helm template cache and in-process Helm-related caches for this run |
| `--destination` | `-d` | Directory to write the packaged chart archive to |

## Examples

```bash
# Package one app into the current directory
hydra local package prod.cluster-infra.cert-manager

# Write the packaged chart into dist/
hydra local package prod.cluster-infra.cert-manager --destination dist

# Package with local-only dependency resolution
hydra local package prod.cluster-infra.cert-manager --helm-network-mode local
```

## When To Use It

Use `hydra local package` when you want the packaged chart artifact itself, not just rendered YAML:

- to compare the packaged chart with CI output
- to inspect the final chart content after `templateFiles` mutations
- to hand a `.tgz` archive to another local Helm-based workflow

If you want rendered manifests instead of a chart archive, use [`hydra local template`](template.md).

## See Also

- [`hydra local template`](template.md) — render the same app to Kubernetes YAML instead of creating a `.tgz`
- [`hydra local source`](source.md) — inspect unrendered template files from disk
- [`hydra ci run publish`](../ci/README.md) — CI packaging and optional OCI upload/signing
