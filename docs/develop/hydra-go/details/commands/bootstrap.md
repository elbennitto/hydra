# Commands: Bootstrap

This file focuses on bootstrap-only SOPS secret conversion and cluster-apply sync behavior.

Back to [Commands detail index](../commands.md).

## Problem

During cluster bootstrap the sops-secrets-operator is not yet running. The operator itself needs secrets (e.g. `image-pull-secret` to pull its container image) that are normally created _by_ the operator from `SopsSecret` custom resources. This creates a chicken-and-egg problem.

## Solution

The `--bootstrap` flag on `hydra gitops apply` has three effects:

1. **SopsSecret decryption**: Decrypts non-backup `SopsSecret` CRs in the rendered manifests and creates additional plain `v1/Secret` resources alongside the original `SopsSecret` CRs. Once the operator starts, it takes ownership of the secrets. **Exception:** SopsSecrets with the `hydra-gitops.org/hydra-backup: "true"` annotation are skipped by conversion. In bootstrap mode they are evaluated by the scoped apply-integrated restore phase instead, and only backup resources that fall outside that selected restore scope must later be excluded from the normal resource apply path. This stricter exclusion is bootstrap-specific for now.

2. **Webhook-aware apply phase plan**: The standard and bootstrap apply phases, webhook handling, and phase logging are documented in [Apply and Webhooks](apply-and-webhooks.md#data-flow-automatically-numbered-bootstrap-apply).

3. **Sync policy**: By default, **`--bootstrap`** uses **`--sync=keep-or-prevent`** unless **`--sync`** is set explicitly (for example **`--sync=default`** to keep template sync). That mode leaves existing cluster `AppProject` sync unchanged and applies the **prevent** mapping only to **new** `AppProject` resources in the apply set—see the user manual for `hydra gitops apply` and `commands.ApplyClusterApplySyncWindowToEntities` in `core/commands/cluster_apply_sync_window.go`.

## PreventSyncWindows

```go
func PreventSyncWindows(l log.Logger, entities entity.Entities, key types.EntityKeyUnstructured) (entity.Entities, error)
```

**Source file:** `core/commands/sync.go` (delegates to `SetAppProjectSyncWindowsWithMutationCount` for prevent sync / full block)

Legacy helper used outside cluster apply. For cluster apply, prefer `ApplyClusterApplySyncWindowToEntities` and related functions in `core/commands/cluster_apply_sync_window.go`.

## Unit Tests (PreventSyncWindows)

1. AppProject with existing syncWindows (kind="allow", manualSync=true) → entries updated to prevent sync (blocked reconciliation)
2. AppProject with syncWindows already in prevent state → unchanged (idempotent)
3. AppProject with `spec.syncWindows` field missing → warning logged, entity returned unchanged
4. AppProject with `spec.syncWindows: []` (empty array) → warning logged, entity returned unchanged
5. Non-AppProject entities → unchanged
6. Mixed entities (AppProject + ConfigMap + Deployment) → only AppProject modified
7. AppProject with multiple syncWindows → all entries set to prevent sync
8. Entity without unstructured data → skipped (returned unchanged)

## Data Flow (SopsSecret Conversion)

```text
hydra gitops apply my-cluster.* --bootstrap
  │
  ▼
1. RenderCluster(cluster, appIds, ...)
   → renderedEntities (contains SopsSecret CRs with encrypted data)
  │
  ▼
2. ConvertSopsSecretsToSecrets(renderedEntities, key, sops.DecryptSopsYaml)
   │  a. Filter entities where Kind == "SopsSecret"
   │  b. Skip SopsSecrets with hydra-gitops.org/hydra-backup: "true" annotation
   │  c. For each remaining SopsSecret entity:
   │       Read AbsPath from entity (set during rendering by enrichEntityPaths)
   │       If AbsPath exists on disk: sops --decrypt <absPath> (original bytes, no MAC mismatch)
   │       Else fallback: serialize Unstructured → YAML, sops --decrypt via stdin
   │       Parse spec.secretTemplates[]
   │       For each secretTemplate → create v1/Secret entity:
   │         metadata.name      = secretTemplate.name
   │         metadata.namespace  = SopsSecret namespace
   │         type               = secretTemplate.type
   │         stringData / data   = secretTemplate.stringData / .data
   │  c. Append new Secret entities to original entities
   │
   → entities (non-backup SopsSecret CRs + plain Secrets + untouched backup SopsSecret CRs)
  │
  ▼
3. Apply phase plan (see "Data Flow (Automatically Numbered Bootstrap Apply)" above)
   → ordinary selected-app `SopsSecret` resources stay in the normal bootstrap
     apply set; only backup resources classified as out-of-scope by the scoped
     restore step are later filtered out
```

## Testability

`ConvertSopsSecretsToSecrets` accepts a `SopsDecryptor` function parameter:

```go
type SopsDecryptor func(yaml types.YamlString) (types.YamlString, error)
```

During rendering, `enrichEntityPaths` in `render.go` computes `AbsPath` and `RepoPath` for each entity based on `TemplatePath`, `AppId.IsRootApp()`, the cluster path, and the git root. At decryption time, `convertSopsSecret` reads `AbsPath` from the entity. If the file exists on disk, `sops.DecryptSopsFile` decrypts the original file directly, avoiding MAC mismatch errors caused by YAML re-serialization. The `SopsDecryptor` function serves as fallback when no `AbsPath` is available (e.g. in tests). In tests a mock decryptor returns pre-decrypted YAML so no SOPS keys or binary are needed.

## Unit Tests

Test scenarios for `ConvertSopsSecretsToSecrets`:

1. Single SopsSecret with one secretTemplate → one additional Secret entity
2. SopsSecret with multiple secretTemplates → multiple additional Secret entities
3. Mixed entities (SopsSecret + ConfigMap + Deployment) → only SopsSecrets converted, others unchanged
4. No SopsSecret entities → entities returned unchanged
5. SopsSecret with `data` field (base64) instead of `stringData` → `data` preserved in output Secret
6. Decryptor returns error → error propagated
7. Backup `SopsSecret` with `hydra-gitops.org/hydra-backup: "true"` → decryptor is not called and no derived plain `Secret` is created
8. Mixed backup and non-backup `SopsSecret`s → only non-backup items are converted; backup items remain untouched for the later bootstrap restore/apply integration step

## Source files

| File                                           | Purpose                                                                                  |
| ---------------------------------------------- | ---------------------------------------------------------------------------------------- |
| `core/commands/bootstrap.go`                   | `ConvertSopsSecretsToSecrets` — conversion logic                                         |
| `core/commands/sync.go`                        | `PreventSyncWindows` — legacy AppProject sync helper                                     |
| `core/commands/cluster_apply_sync_window.go`   | `ApplyClusterApplySyncWindowToEntities` — cluster apply sync policy                      |
| `core/sops/sops.go`                            | `DecryptSopsFile` / `DecryptSopsYaml` — SOPS decryption                                  |
| `cli/flags/bootstrap.go`                       | `BootstrapFlag` type                                                                     |
| `cli/cmd/define_flags.go`                      | `defineBootstrapFlag()` registration                                                     |
| `cli/action/cluster_apply.go`                  | Integration point for bootstrap conversion and hand-off into the shared apply phase plan |

## Examples

See `examples/bootstrap-sopssecret.yaml` (input) and `examples/bootstrap-secret.yaml` (output).
