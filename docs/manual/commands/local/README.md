# Local Commands

Commands that render and inspect templates without connecting to a Kubernetes cluster.

## Contents

| Command | Description |
|---------|-------------|
| [template](template.md) | Render Helm templates |
| [package](package.md) | Package one app chart into a Helm `.tgz` |
| [apps](apps.md) | Resolve app-id patterns to matching apps |
| [find](find.md) | Query rendered resources with CEL filters |
| [source](source.md) | Print unrendered template files from disk |
| [config](config.md) | Show global.hydra configuration |
| [values](values.md) | Display computed Helm values |
| [list](list.md) | Print sorted resource IDs |
| [refs](refs.md) | List transitive references |
| [inspect](inspect.md) | Interactive TUI for browsing references |
| [review](review.md) | Validate rendered refs and ownership |
| [test](test.md) | Run application tests (`test refs` for ref-parser golden files) |
| [export](export.md) | Export dependency model for all clusters |

## Common Usage

```bash
# Render a single app
hydra local template prod.cluster-infra.ingress-nginx

# Package the same app chart locally
hydra local package prod.cluster-infra.ingress-nginx --destination dist

# List all apps of cluster prod
hydra local apps 'prod.**'

# Render all apps on a cluster
hydra local template 'prod.**'

# Search rendered resources
hydra local find 'prod.**' --include 'kind == "Deployment"' --pick 'name' --uniq

# Browse interactively
hydra local inspect prod
```

## No Cluster Required

All `hydra local` commands work entirely offline. They render from the charts-repository and gitops-repository without contacting any Kubernetes API. This makes them safe for development and CI environments.
