# Hydra user configuration (kubeconfig mapping)

Hydra optionally reads a small YAML file in your XDG configuration directory to map **repository cluster directories** to a specific **kubeconfig file** and **kubectl context name**. When a mapping applies, Hydra uses that kubeconfig for Kubernetes API access and for `global.hydra.kubectl.allowedContexts` checks, instead of the default kubeconfig loader chain (including `KUBECONFIG`).

This does **not** replace the Hydra GitOps context: you still must pass `--hydra-context <path>` or set `HYDRA_CONTEXT` (the flag wins when both are set), as for all other commands.

## Config file location

| Variable | Effect |
| -------- | ------ |
| `XDG_CONFIG_HOME` | If set and non-empty, the file is `$XDG_CONFIG_HOME/hydra/config.yaml`. |
| (unset) | Defaults to `$HOME/.config/hydra/config.yaml`. |

The file is **optional**. If it is missing, Hydra behaves as before.

## YAML format

```yaml
contexts:
  - path: /absolute/path/to/gitops/clusters/env/my-cluster
    config: /home/you/.kube/config-prod
    name: my-admin-context
```

| Field | Meaning |
| ----- | ------- |
| `path` | Filesystem path to the **cluster directory** inside your Hydra context (same layout as on disk: `<hydra-context>/<clusterName>`). Use absolute paths for stable matching. |
| `config` | Path to the kubeconfig file to use for this cluster. |
| `name` | Context name **within that kubeconfig file** to activate (same as `kubectl config use-context`). |

The **first** list entry whose `path` matches the resolved cluster directory is used. As a fallback, Hydra also accepts `path` set to the **Hydra context directory** (the parent of `<hydra-context>/<clusterName>`), so one entry can cover every cluster under that context. That fallback is discouraged: Hydra logs a **WARN** asking you to switch to an explicit cluster directory path so the mapping stays clear and does not silently apply the same kube credentials to multiple clusters by mistake.

If the file exists but cannot be parsed, Hydra logs a warning and continues **without** applying any mapping (same as no file).

For every non-empty `contexts[].path`, Hydra checks that the path exists and is a **directory** (after resolving relative paths against the current working directory). For every non-empty `contexts[].config`, Hydra checks that the path exists and is **not a directory** (expected kubeconfig file). Each invalid value produces a **WARN**; mapping may still apply for other entries if one matches the current cluster.

## Interaction with `allowedContexts`

`global.hydra.kubectl.allowedContexts` in your GitOps Helm values is **always** enforced against the **effective** kubeconfig context (after applying this mapping). The user file cannot disable or bypass that check.

## Logging

When a mapping is applied for the current cluster, Hydra emits an **INFO** log line that includes the user config path, the kube context name, and the kubeconfig file path.

If the match used a **Hydra context directory** in `path` (parent-directory fallback), Hydra emits a **WARN** recommending that you set `path` to the full cluster directory instead.

## See also

- [`hydra gitops validate-current-context`](cluster/validate-current-context.md) — uses the same kubeconfig resolution as other cluster commands.
- [CLI overview](README.md#hydra-context) — Hydra context via `--hydra-context` / `HYDRA_CONTEXT`.
