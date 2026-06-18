# hydra local values

Display the computed Helm values for an application.

## Synopsis

```text
hydra local values <appId> [flags]
```

## Description

Renders and displays the fully resolved Helm values for a given application. This shows the merged result of all value sources in the Hydra context hierarchy (global defaults, cluster-level overrides, app-level values). **`global.hydra` here is only what Helm values provide** — it does not include Hydra ConfigMap `data.hydra` overlays from rendered charts.

Use this to understand which values are active for an app before rendering templates. This is the Hydra equivalent of inspecting the `-f` values files you'd pass to `helm install`. When you need **`global.hydra` after merging chart ConfigMaps from every app on the cluster**, use [`hydra gitops values`](../cluster/values.md) instead.

## When To Use It

Use `hydra local values` before `hydra local template` when you need to debug value layering:

- Which override won?
- What does this app actually receive after merge?
- Is a cluster-specific setting present before I look at manifests?

## Arguments

| Argument | Description                                        |
| -------- | -------------------------------------------------- |
| `appId`  | A single [App ID](../README.md#app-ids) to inspect |

## Flags

| Flag | Short | Description |
| --- | --- | --- |
| `--hydra-context` | | Path to the [Hydra context directory](../README.md#hydra-context) (or set `HYDRA_CONTEXT` env var) |
| `--color` | `-c` | Force colored output |
| `--no-color` | | Disable colored output |
| `--color-mode` | | Color mode: `auto`, `always`, or `never` |
| `--helm-network-mode` | | [Helm network mode](../README.md#helm-network-mode): `online`, `local`, `offline`, or `error` |
| `--no-cache` | | Disable persistent Helm template cache and in-process Helm-related caches for this run |

## Examples

```bash
# Show computed values for an app
hydra local values prod.infra.cert-manager

# With colored YAML output
hydra local values prod.infra.cert-manager --color

# Pipe to yq for querying specific paths
hydra local values prod.infra.cert-manager | yq '.global.hydra'

# Compare values between environments
diff <(hydra local values staging.infra.cert-manager) <(hydra local values prod.infra.cert-manager)

# Inspect one subtree only
hydra local values prod.apps.my-service | yq '.image'
```

## Tutorials

- [Adding Manifests to the Helm Charts](../../tutorials/introduction/02-00-adding-manifests-to-the-helm-charts.md) — uses `hydra local values` to inspect the merged Helm values before looking at manifests

## See Also

- [`hydra gitops values`](../cluster/values.md) — same Helm merge as this command, but `global.hydra` includes Hydra ConfigMaps from a full-cluster render
- [`hydra local config`](config.md) — show `global.hydra` and Hydra ConfigMaps from the render
- [`hydra local template`](template.md) — see the manifests produced from these values
