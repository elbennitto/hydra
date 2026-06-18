# Values

All configurable options in Hydra's value system. Covers the `global.hydra` block, value layering, and every available configuration key.

## Contents

- [Overview](overview.md) — Value layering and merge semantics
- [global.hydra Reference](global-hydra.md) — Complete structure of the global.hydra block
- [refs](refs-in-values.md) — Defining dependency edges in values
- [presets](presets-in-values.md) — Overriding preset configuration
- [clones](clones-in-values.md) — Runtime resource copying configuration
- [templatePatches](template-patches.md) — YQ-based post-render mutations
- [scale](scale.md) — Workload scaling configuration
- [diff](diff.md) — Diff ignore rules
- [ready](ready.md) — Readiness probes via CEL
- [ownerNamespaces](owner-namespaces.md) — Namespace ownership declaration
- [kubectl](kubectl.md) — Allowed contexts validation
- [uninstall-finalizer](uninstall-finalizer.md) — Custom uninstall finalizers

## Prerequisites

- [Concepts: Repositories](../concepts/repositories.md) — Understand where values live
- [Concepts: Hydra ConfigMaps](../concepts/hydra-configmaps.md) — Runtime value injection
