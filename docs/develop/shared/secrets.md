# Secrets in Hydra YAML

Hydra models Kubernetes Secrets and SOPS-encrypted secrets as entities in the dependency graph. No actual secret **values** are stored — only structural metadata (key names, relationships, origin).

## Key Concepts

- **Secret entities** — `v1/Secret` with optional `secretKeys` (key names from `data`/`stringData`, never values)
- **SopsSecret entities** — `isindir.github.com/v1alpha3/SopsSecret` custom resources managed by the SOPS Secrets Operator
- **SopsSecret → Secret relationship** — modeled **generically** as “CR → `v1/Secret` in the **same namespace**” using the relation attribute **`"origin:generated": controller`** (and optional labels such as `sops` for display) and `reverse: true`, declared by **app ref-parser metadata** (Helm `global.hydra.refs` / merged ConfigMap parsers), not by Hydra maintaining a one-off hard-coded mapping in application code; virtual/generated Secrets carry parser-defined provenance via repeated relation attributes such as `origin:app` and `origin:workload` instead of a fixed controller tag baked into Go
- **Chains and consumers** — Any feature that needs to know whether a `Secret` exists may follow refs with **`"origin:generated": job`** or **`"origin:generated": controller`** recursively to a fixpoint; this is **not** specific to one CLI command. External policy that **mirrors** secrets (for example Kyverno) still selects **`v1/Secret`** as the object to clone—Hydra’s graph keeps the `SopsSecret` for provenance, but policies typically target the **Secret**
- **Consumer references** — labels `volume`, `env`, `envFrom`, `imagePullSecret` indicate how workloads use Secrets
- **Missing secrets** — tagged `app:missing` when referenced but not present in rendered templates; have no `secretKeys`
- **UI display** — Details tab shows `secretKeys` as badges; Secrets tab shows producer, key table, and consumers

**Source files:** `src/model.ts`, `src/parseHydra.ts`, `src/components/EntityPage.tsx`

→ **Full details:** [details/secrets.md](details/secrets.md)
