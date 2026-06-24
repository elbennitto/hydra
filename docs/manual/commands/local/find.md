# hydra local find

Query rendered Hydra resources and project selected values as a YAML array.

## Synopsis

```bash
hydra local find <appId> [appId...] [flags]
```

## Description

Renders the selected Hydra applications locally, filters the resulting Kubernetes resources with CEL predicates, and then evaluates a CEL projection for each match.

Unlike [`hydra local template`](template.md), this command is not meant to print full manifests for downstream tools. Instead, it answers targeted questions such as:

- Which child apps render a `KafkaUser`?
- Which resource names match a predicate?
- Which app/resource combinations satisfy a condition?

The command is local and read-only. It does not connect to Kubernetes.

`--pick` exists only on `hydra local find`. It evaluates a CEL expression per matched resource and serializes the results as a single YAML array. If omitted, it defaults to `id`.

## When To Use It

Use `hydra local find` when you want structured answers from rendered resources:

- Find which apps contain a certain resource kind
- Extract only names, namespaces, or App IDs instead of full manifests
- Produce machine-friendly YAML arrays for shell pipelines or review

Use [`hydra local template`](template.md) when you need the full rendered manifests instead of a query result.

## Arguments

| Argument | Description                                                                                |
| -------- | ------------------------------------------------------------------------------------------ |
| `appId`  | One or more [App IDs](../README.md#app-ids) (supports [wildcards](../README.md#wildcards)) |

## Flags

| Flag | Short | Description |
| --- | --- | --- |
| `--hydra-context` | | Path to the [Hydra context directory](../README.md#hydra-context) (or set `HYDRA_CONTEXT` env var) |
| `--color` | `-c` | Force colored output |
| `--no-color` | | Disable colored output |
| `--color-mode` | | Color mode: `auto`, `always`, or `never` |
| `--helm-network-mode` | | [Helm network mode](../README.md#helm-network-mode): `online`, `local`, `offline`, or `error` |
| `--no-cache` | | Disable persistent Helm template cache and in-process Helm-related caches for this run |
| `--exclude-app` | | Glob pattern to exclude applications (repeatable) |
| `--include` | `-i` | [CEL expression](../README.md#cel-resource-filters) to filter rendered resources |
| `--exclude` | `-e` | [CEL expression](../README.md#cel-resource-filters) to exclude rendered resources |
| `--pick` | | CEL expression to project each matched resource into the output array (default: `id`) |
| `--uniq` | | Deduplicate projected values after `--pick` evaluation |

## Projection Rules

`--pick` should return a YAML-serializable value:

- scalar values such as strings, numbers, or booleans
- lists such as `appIds`
- maps such as `{"appId": appIds[0], "kind": kind}`

Common fields available in expressions include:

- `appIds`
- `appNamespace`
- `kind`
- `name`
- `ns`
- `gvk`
- `templateEntity`
- `templatePath`
- `repoPath`
- `absPath`

## Examples

```bash
# Which child apps render KafkaUser resources?
hydra local find prod.*.* --include 'kind == "KafkaUser"' --pick 'appIds[0]' --uniq

# Query all apps across all clusters
hydra local find ** --include 'kind == "Deployment"' --pick 'appIds[0]' --uniq

# Return app/resource pairs instead of plain strings
hydra local find prod.*.* --include 'kind == "KafkaUser"' --pick '{"appId": appIds[0], "name": name}'

# List Secret names except those in kube-system
hydra local find prod.** --exclude 'ns == "kube-system"' --include 'kind == "Secret"' --pick 'name' --uniq
```

## See Also

- [`hydra local template`](template.md) — render full manifests for selected apps
- [`hydra local values`](values.md) — inspect the merged values that feed rendering
- [`hydra gitops diff`](../cluster/diff.md) — compare rendered output against live cluster state
