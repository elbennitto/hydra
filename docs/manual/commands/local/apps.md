# hydra local apps

Resolve app-id patterns against the Hydra context and print matching app ids.

## Synopsis

```text
hydra local apps [appId...] [flags]
```

## Description

`hydra local apps` resolves one or more [App ID patterns](../README.md#app-ids) against the current Hydra context and prints the matching app ids, one per line.

Unlike [`hydra local template`](template.md) or [`hydra local find`](find.md), this command does not render Helm charts. It only performs app selection, including `--exclude-app`, and does not connect to a Kubernetes cluster.

When no `appId` pattern is provided, Hydra uses `**` and lists all apps found in the context.

This makes the command useful as the safest first check when you want to verify that Hydra can discover the apps you expect before moving on to rendering or cluster operations.

## Arguments

| Argument | Description |
| -------- | ----------- |
| `appId` | Optional App ID pattern(s). Supports [wildcards](../README.md#wildcards). If omitted, Hydra uses `**`. |

## Flags

| Flag | Short | Description |
| --- | --- | --- |
| `--hydra-context` | | Path to the [Hydra context directory](../README.md#hydra-context) (or set `HYDRA_CONTEXT` env var) |
| `--helm-network-mode` | | [Helm network mode](../README.md#helm-network-mode): `online`, `local`, `offline`, or `error` |
| `--no-cache` | | Disable persistent Helm template cache and in-process Helm-related caches for this run |
| `--exclude-app` | | Glob pattern to exclude applications from selection (repeatable) |

## Examples

```bash
# List all apps in the current context
hydra local apps

# List all apps below one cluster
hydra local apps prod.**

# List only child apps below one root app
hydra local apps prod.cluster-infra.*

# Exclude one app from the result set
hydra local apps in-cluster.** --exclude-app in-cluster.apps.argocd
```

## Tutorials

- [Create a Hydra App](../../tutorials/introduction/01-00-create-a-hydra-app.md) — uses `hydra local apps` to validate the context layout and verify app discovery

## See Also

- [`hydra local template`](template.md) — render manifests for the selected apps
- [`hydra local find`](find.md) — query rendered resources from the selected apps
- [`hydra local list`](list.md) — print rendered Hydra resource ids for the selected apps
- [`hydra gitops apps`](../cluster/README.md) — cluster command group that currently exposes the same app-id resolution behavior under `gitops`
