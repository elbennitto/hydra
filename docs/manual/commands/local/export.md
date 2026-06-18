# hydra local export

Export the dependency model, rendered manifests, and charts for all clusters in the context.

## Synopsis

```text
hydra local export <output-dir> [flags]
```

## Description

Renders all clusters in the Hydra context and exports the complete dependency model, Kubernetes manifests, and Helm charts to the specified output directory. This produces a full snapshot of everything Hydra manages.

The primary use case is generating the dataset for **Hydra UI**, the web frontend that visualizes the dependency graph, entity details, RBAC configuration, and values. Run `hydra local export`, then load the output directory in Hydra UI for interactive exploration.

## Arguments

| Argument     | Description                                                            |
| ------------ | ---------------------------------------------------------------------- |
| `output-dir` | Directory to write the exported files to (created if it doesn't exist) |

## Flags

| Flag | Short | Description |
| --- | --- | --- |
| `--hydra-context` | | Path to the [Hydra context directory](../README.md#hydra-context) (or set `HYDRA_CONTEXT` env var) |
| `--color` | `-c` | Force colored output |
| `--helm-network-mode` | | [Helm network mode](../README.md#helm-network-mode): `online`, `local`, `offline`, or `error` |
| `--no-cache` | | Disable persistent Helm template cache and in-process Helm-related caches for this run |
| `--crd-mode` | | CRD handling: `error` or `ignore` |

## Examples

```bash
# Export all clusters
hydra local export ./hydra-export

# Export offline (no chart downloads)
hydra local export ./hydra-export --helm-network-mode offline

# Export and immediately serve in Hydra UI
hydra local export ./hydra-export && cd hydra-ui && yarn dev
```

## See Also

- [`hydra local template`](template.md) — render a single app's manifests (lighter alternative)
- [`hydra local values`](values.md) — inspect values for a single app
