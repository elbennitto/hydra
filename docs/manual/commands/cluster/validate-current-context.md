# hydra gitops validate-current-context

Verify that the active kubeconfig context matches the expected cluster.

## Synopsis

```text
hydra gitops validate-current-context <cluster> [flags]
```

## Description

Safety check that prevents accidentally running commands against the wrong cluster. Compares the active kubeconfig context against allowed contexts configured in the Hydra values.

All other `hydra gitops` commands perform this validation automatically before connecting. Use this command explicitly as the first step in scripts or when switching between clusters.

If you use the optional [user kubeconfig mapping](../configuration.md), this command validates the **mapped** kubeconfig and context (the same effective loader as `hydra gitops diff`, `apply`, and other API commands).

For operators who frequently jump between clusters, this is the "seatbelt" command. Run it before any destructive or mutating workflow.

## Configuration

Allowed contexts are configured in the Hydra values under `global.hydra.kubectl.allowedContexts`. Each entry matches on one or more kubeconfig fields:

| Field      | Description                        |
| ---------- | ---------------------------------- |
| `name`     | The kubeconfig context name        |
| `cluster`  | The kubeconfig cluster name        |
| `authInfo` | The kubeconfig user/auth-info name |

At least one entry is required. Each field within an entry is optional — only specified fields are checked. The current context must match **all specified fields** of **at least one entry**.

```yaml
global:
  hydra:
    kubectl:
      allowedContexts:
        - name: arn:aws:eks:eu-central-1:123456789:cluster/prod
        - cluster: prod-cluster
          authInfo: admin@prod
```

## Arguments

| Argument  | Description                                                            |
| --------- | ---------------------------------------------------------------------- |
| `cluster` | The cluster name (as defined in the Hydra context) to validate against |

## Flags

| Flag | Short | Description |
| --- | --- | --- |
| `--hydra-context` | | Path to the [Hydra context directory](../README.md#hydra-context) (or set `HYDRA_CONTEXT` env var) |
| `--color` | `-c` | Force colored output |
| `--no-color` | | Disable colored output |
| `--color-mode` | | Color mode: `auto`, `always`, or `never` |
| `--no-cache` | | Disable persistent Helm template cache and in-process Helm-related caches for this run |

## Examples

```bash
# Validate before running cluster commands
hydra gitops validate-current-context prod

# Recommended first command after switching kubeconfig context
kubectl config use-context prod-admin
hydra gitops validate-current-context prod

# Use in scripts
hydra gitops validate-current-context prod && hydra gitops apply prod.infra.*

# With explicit Hydra context
hydra gitops validate-current-context prod --hydra-context /path/to/context
```

## See Also

- [`hydra gitops apply`](apply.md) — includes context validation automatically
- [`hydra gitops diff`](diff.md) — includes context validation automatically
