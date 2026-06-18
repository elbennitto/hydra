# hydra local source

Print unrendered Helm chart template files from disk for one or more apps. This command does **not** run the Hydra render pipeline and does **not** output Kubernetes manifests.

## Synopsis

```text
hydra local source <appId> [appId...] [flags]
```

## Description

For each selected app, Hydra loads the chart with the same Helm loader as rendering (including **packaged** dependencies under `charts/*.tgz`). Template bodies and `# Source:` paths come from that loaded chart, not only from loose files on disk — so umbrella charts without a top-level `templates/` directory still show their dependency templates.

`# Source:` paths are Helm’s internal template file names (usually forward slashes), which may differ from a raw repository directory layout.

Bodies may contain Helm directives (`{{ … }}`); the stream is **not** valid Kubernetes YAML.

When `--color` / `--color-mode` request colored terminal output, Hydra highlights the template body with [Chroma](https://github.com/alecthomas/chroma): delegating lexer **YAML + go-text-template** (YAML in the gaps between `{{ … }}`, template actions highlighted as Go templates).

Use `--exclude-app` to remove specific apps from the selection (same glob patterns as other multi-app local commands).

Use `--include` and `--exclude` (repeatable CEL expressions) to apply the same rendered-manifest filtering logic as [`hydra local template`](template.md). Hydra first renders the manifests, keeps only the resources matching the CEL selection, then collects the corresponding template source files and prints only those source files.

This means the filters do **not** print rendered Kubernetes YAML here. They only decide **which source templates** are shown.

Use `--include-path` (repeatable) to print only files whose **Helm template path** (as in `# Source:`) matches in either of these ways:

1. **Prefix from the chart root** (path boundary after the prefix): the path equals the prefix, or the next character is `/`.
2. **Segment path inside the full Helm name** (only when the flag value contains at least one `/`): the path contains `/<your-prefix>/` or ends with `/<your-prefix>`. This covers umbrella charts where Helm prefixes template names (for example `# Source: kube-prometheus-stack/charts/kube-prometheus-stack/templates/prometheus/...`): you can still pass `charts/kube-prometheus-stack/templates/prometheus` or the full path from the error message.

So `templates/foo` matches `templates/foo/bar.yaml` but does **not** match `templates/foobar.yaml`. To match a single file, pass the full chart-relative path including the file name (for example `templates/kafkauser-a.yaml`), or a multi-segment suffix such as `templates/prometheus/clusterrole.yaml` when that sequence appears in the Helm path.

Multiple `--include-path` values are combined with **OR** semantics.

If you combine `--include` / `--exclude` with `--include-path`, Hydra intersects both selections: the source file must be selected by the rendered-manifest filter **and** match one of the path filters.

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
| `--include` | `-i` | CEL expression to keep matching rendered resources; `local source` then prints only the backing source files |
| `--exclude` | `-e` | CEL expression to remove matching rendered resources before collecting source files |
| `--include-path` | | Helm template path filter (repeatable, OR): prefix from chart root or contained multi-segment path; see [Description](#description) |

## Examples

```bash
# All template files for one app
hydra local source prod.infra.monitoring --hydra-context /path/to/context

# Only source templates that produced Deployment manifests
hydra local source prod.apps.my-service --include 'kind == "Deployment"'

# Everything except source templates that produced Service manifests
hydra local source prod.apps.my-service --exclude 'kind == "Service"'

# Only files under a dependency’s prometheus templates directory
hydra local source prod.infra.prom --include-path charts/kube-prometheus-stack/templates/prometheus

# Combine rendered-manifest filtering with path filtering
hydra local source prod.infra.prom \
  --include 'kind == "Prometheus"' \
  --include-path charts/kube-prometheus-stack/templates/prometheus

# Multiple prefixes
hydra local source prod.infra.prom \
  --include-path charts/kube-prometheus-stack/templates/prometheus \
  --include-path charts/kube-prometheus-stack/templates/alertmanager
```

## Tutorials

- [Adding Manifests to the Helm Charts](../../tutorials/introduction/02-00-adding-manifests-to-the-helm-charts.md) — uses `hydra local source` to inspect source templates, including dependency charts

## See Also

- [`hydra local template`](template.md) — rendered Kubernetes manifests from the Hydra pipeline
- [`hydra local find`](find.md) — query **rendered** resources with CEL filters
