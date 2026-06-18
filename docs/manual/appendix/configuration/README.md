# Configuration

How to configure Hydra's own behavior and environment.

## Contents

| File | Description |
|------|-------------|
| [HYDRA_CONTEXT](hydra-context.md) | Select the active Hydra context directory |
| [config.yaml](config-yaml.md) | Optional per-user kubeconfig mapping (XDG) |
| [Kubernetes Context](kubernetes-context.md) | kubectl context requirements for `hydra gitops` |
| [Cluster Overrides](cluster-overrides.md) | Optional `hydra.yaml` for manual live-resource assignment per cluster |
| [CLI Flags](cli-flags.md) | Global flags on all commands |
| [.hydra-ci.yaml](hydra-ci-yaml.md) | CI pipeline configuration for `hydra ci` |

## Quick Start

```bash
# Set the Hydra context once per shell
export HYDRA_CONTEXT=/path/to/hydra/context

# Or pass it per command
hydra --hydra-context /path/to/hydra/context local template 'prod.**'
```

## See Also

- [Concepts: Context and Clusters](../concepts/context-and-clusters.md)
- [Command reference](../../commands/README.md)
