# hydra gitops dump

Retrieve and display Kubernetes resources from a live cluster as YAML.

## Synopsis

```text
hydra gitops dump <cluster> [flags]
```

## Description

Connects to a Kubernetes cluster and retrieves the current state of Hydra-managed resources. This is the command to use when you want Hydra's live view of the cluster, independent of what the current context would render.

The command reads that live side from the shared resource model used across cluster commands. In practice, Hydra still lists the live API objects once, but the dump reader consumes normalized per-ID inventory entities rather than a separate command-local live-only collection.

**Output** is always a **multi-document YAML stream**: each document is a full live object, separated by `---`. Each document is prefixed with a YAML comment containing the Hydra resource id.

For a Kubernetes admin, `dump` is the closest Hydra equivalent to a scoped `kubectl get ... -o yaml`, but limited to resources managed through Hydra's model.

## When To Use It

Use `hydra gitops dump` when you need to inspect live objects directly:

- Verify what is currently running after an apply
- Compare live resources with what [`hydra local template`](../local/template.md) would produce
- Feed live YAML into other tooling

Use [`hydra gitops list`](list.md) when you only need resource ids (one per line).

Use [`hydra gitops diff`](diff.md) when your goal is review of desired vs live state rather than raw inspection.

## Arguments

| Argument  | Description                                        |
| --------- | -------------------------------------------------- |
| `cluster` | The cluster name (as defined in the Hydra context) |

## Flags

| Flag | Short | Description |
| --- | --- | --- |
| `--hydra-context` | | Path to the [Hydra context directory](../README.md#hydra-context) (or set `HYDRA_CONTEXT` env var) |
| `--color` | `-c` | Force colored output |
| `--no-color` | | Disable colored output |
| `--color-mode` | | Color mode: `auto`, `always`, or `never` |
| `--include` | `-i` | [CEL expression](../README.md#cel-resource-filters) to filter resources |
| `--exclude` | `-e` | [CEL expression](../README.md#cel-resource-filters) to exclude resources |
| `--no-cache` | | Disable persistent Helm template cache and in-process Helm-related caches for this run |

## Examples

```bash
# Full YAML dump for inspection or scripting
hydra gitops dump prod

# Narrow the dump to a single namespace
hydra gitops dump prod --include 'namespace == "cert-manager"'

# Dump only Deployments
hydra gitops dump prod --include 'kind == "Deployment"'

# Dump everything except kube-system
hydra gitops dump prod --exclude 'namespace == "kube-system"'

# Pipe live YAML to other tools
hydra gitops dump prod --include 'kind == "Secret"' | yq 'select(.type == "kubernetes.io/tls") | .metadata.name'
```

## See Also

- [`hydra gitops list`](list.md) — list resource ids only
- [`hydra gitops diff`](diff.md) — compare live state against rendered templates
- [`hydra local template`](../local/template.md) — see what Hydra would render (without cluster)
