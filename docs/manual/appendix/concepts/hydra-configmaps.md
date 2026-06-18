# Hydra ConfigMaps

Hydra **ConfigMaps** are normal Kubernetes `v1/ConfigMap` objects that carry Hydra configuration under `data.hydra` and are marked with the annotation `hydra-gitops.org/hydra-config: "true"`. They extend or override Helm `global.hydra` for `hydra gitops` commands (refs, clones, `templatePatches`, `scope`, and related blocks). Shape and conventions are covered alongside [`hydra local config`](commands/local/config.md) and [`hydra gitops values`](commands/cluster/values.md).

## Source Of Truth By Command Surface

- **`hydra local`** reads only from the local Hydra context. Hydra ConfigMaps matter only insofar as they are rendered locally like any other manifest.
- **`hydra gitops`** also uses the **local Hydra context** as its source of truth. Hydra ConfigMap documents come from **`helm template` output** and are then exported into cluster ConfigMaps as part of the rendered manifests.
- **`hydra cluster`** is reserved for future cluster-only workflows. Because those commands will not use local files, the Hydra ConfigMaps already stored in Kubernetes will become their configuration source.

For current **mutating** `hydra gitops` workflows—[`hydra gitops apply`](commands/cluster/apply.md), [`hydra gitops uninstall`](commands/cluster/uninstall.md), generating manifests with [`hydra gitops template`](commands/cluster/template.md), and similar—Hydra **does not** load these ConfigMaps from the API to decide what to write or how to merge rules. The cluster is not the source of truth for that logic.

After `hydra gitops apply`, the same objects **exist in the cluster** as they would for any other rendered manifest. Think of them as a **documented snapshot** of the Hydra config that was shipped with the charts—useful for operators and tooling, but **not** an input channel back into current GitOps write paths.

## Template render vs. live cluster

Hydra uses two different notions of “compare Git to cluster”; both include Hydra ConfigMaps in the **template** side when those objects are part of the render.

| Intent | Command | What is compared |
| --- | --- | --- |
| **Manifest drift** — will the next apply change YAML, or what changed since the last sync? | [`hydra gitops diff`](commands/cluster/diff.md) | **Unified diff**: each resource’s **rendered manifest** (from `helm template` in your Hydra context) vs. the **live object** in the API for the same id. Hydra ConfigMaps appear here like any other rendered resource. |
| **Reference and ownership review** — do refs from deployed resources point at real objects, and who “owns” cluster-only noise? | [`hydra gitops review`](commands/cluster/review.md) | **Sources** — template entities (and clone materializations) from the selected apps, normalized and usually filtered to ids that **already exist on the cluster** — vs. **targets** — the **full live inventory** from the API. Findings are ref validation and ref-ownership rules, not a line-by-line YAML diff. |

So: use **`hydra gitops diff`** when you care about **document-level** drift (including Hydra ConfigMap bodies). Use **`hydra gitops review`** when you care about **graph-level** consistency (refs, keys, ownership) between what you render and what the cluster actually holds.

## Using cluster ConfigMaps without Git

Programs that embed Hydra **as a library** and run reviews or checks **without** access to the Git repository may still need Hydra-shaped configuration. In those situations, the **Hydra ConfigMaps already stored in the cluster** can serve as a **read-only source** for `data.hydra` content, since they mirror what was applied from templates. This matches the direction of the future `hydra cluster` command surface. It does not change current CLI behavior: today `hydra gitops` still merges ConfigMaps from **rendered charts** in your workspace, not from `kubectl get` during apply or uninstall.

## See also

- [`hydra local config`](commands/local/config.md) — Helm `global.hydra` only; ConfigMap merge is `hydra gitops` territory today
- [`hydra gitops values`](commands/cluster/values.md) — merged `global.hydra` including Hydra ConfigMaps from the full-cluster template catalog
- [Charts repository — main concepts](concepts/charts-repository.md#main-concepts) — where charts may define these ConfigMaps
