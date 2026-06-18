# Cluster Dump Directory Structure

The `hydra ui <dir>` command exports the full dependency model, rendered manifests, and Helm chart archives for **all clusters** of a hydra context into a structured directory. The Hydra UI serves this directory to display manifests and Go templates alongside the dependency graph.

## Key Concepts

- **Per-cluster subdirectories** — each cluster gets its own `<dir>/<cluster>/` folder containing `hydra.yaml`, `charts/`, `manifests/`, and `values/`
- **Root app merge** — root apps from non-in-cluster clusters produce ArgoCD Application CRs that are merged into the `in-cluster` export
- **Single-pass render with dual split** — each cluster is rendered once; entities are then split into root-app and child-app groups
- **Manifest paths** — entities carry a `manifestPath` field matching the filesystem layout: `<appId>/<group>/<version>/<kind>/<namespace>/<name>.yaml`
- **Value files hierarchy** — `values/` contains `files/` (from GitOps repo), `fallback/` (from infra_library), and `merged/` (final merged values per app)
- **Chart archives** — one `.tgz` per unique chart name in `charts/`
- **Directory validation** — existing cluster subdirectories with the expected structure are cleared and re-populated; other existing directories cause an error

**Source:** `cli/action/cluster_view.go`, `core/export/export.go`

→ **Full details:** [details/cluster-dump-structure.md](details/cluster-dump-structure.md)
