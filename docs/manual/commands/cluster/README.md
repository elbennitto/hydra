<!-- markdownlint-disable MD013 -->

# GitOps Commands

Commands that use the local Hydra context as the source of truth and connect to the Kubernetes API for live operations.

## Contents

| Command | Description |
| ------- | ----------- |
| [validate-current-context](validate-current-context.md) | Verify kubeconfig matches cluster |
| [dump](dump.md) | Fetch all live resources |
| [list](list.md) | Print resource IDs from cluster |
| [template](template.md) | Render with cluster context |
| [values](values.md) | Show values including ConfigMaps |
| [refs](refs.md) | List transitive references (live) |
| [diff](diff.md) | Compare desired vs live state |
| [apply](apply.md) | Deploy resources via server-side apply |
| [uninstall](uninstall.md) | Remove Hydra-managed resources |
| [status](status.md) | Check per-app sync status |
| [review](review.md) | Validate refs against live API |
| [system](system.md) | Show preset matches against inventory |
| [show](show.md) | Show central live-resource app assignment |
| [untracked](untracked.md) | List unmanaged resources |
| [inspect](inspect.md) | Interactive TUI (live) |
| [scale](scale.md) | Scale workloads up/down |
| [sync](sync.md) | Control ArgoCD reconciliation |
| [backup](backup.md) | Manage backups |
| [cert-manager](cert-manager.md) | Certificate management |

## Common Usage

```bash
# Verify connection
hydra gitops validate-current-context prod

# Check what would change
hydra gitops diff 'prod.**'

# Apply changes
hydra gitops apply 'prod.cluster-infra.ingress-nginx'

# Check status
hydra gitops status 'prod.**'
```

## Command Surfaces

- `hydra local` works only with the local Hydra definitions in your workspace.
- `hydra gitops` works with both the local Hydra definitions and the Kubernetes cluster.
- `hydra cluster` is reserved for future cluster-only workflows where the local state is not available.

## Cluster Connection

All `hydra gitops` commands require a valid Kubernetes connection. Run `hydra gitops validate-current-context <cluster>` first to verify.

See [Configuration: Kubernetes Context](../../appendix/configuration/kubernetes-context.md) for setup.

For the shared data-preparation flow behind many `hydra gitops` commands, see [Concepts: Cluster Command Data Model](../../appendix/concepts/cluster-command-data-model.md).
