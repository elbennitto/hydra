# Hydra YAML File Format

The `hydra.yaml` file is the serialized dependency graph that Hydra generates from rendered Helm templates. It describes Kubernetes entities, their groupings, references between them, and chart metadata. The Hydra UI loads this file to visualize the dependency graph.

## Key Concepts

- **Entities** — each represents a Kubernetes resource with a fully qualified ID (`group/version/kind/namespace/name`), optional `appIds`, `tags`, `templatePath`/`templateIndex`, `manifestPath`, `rbacRules`, and `secretKeys`
- **Groups** — clusters of related entities for visual grouping; only groups with >1 entity are included
- **References** — directed edges between entities with labels (e.g. `serviceAccount`, `volume`, `env`) and an optional `reverse` flag that swaps `from`/`to` at parse time
- **Charts** — optional section with Helm chart metadata (name, version, dependencies) per `appId`
- **Tags** — `app:missing` marks entities referenced but not defined in any template
- **Generation pipeline** — `helm.Template()` → `SplitManifestMap()` → `NewEntitiesFromYaml()` → `ToModel()` → `RenderDependencies()` → `hydra.yaml`

**Source files:** `core/view/dependencies.go` (Go types), `src/parseHydra.ts` (TypeScript parsing)

→ **Full details:** [details/hydra-yaml.md](details/hydra-yaml.md)
