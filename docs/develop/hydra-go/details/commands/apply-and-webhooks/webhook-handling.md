# Apply and Webhooks: Webhook Handling

This page covers webhook provider resolution, rule matching, and the logic that decides when webhook configurations must be disabled.

Back to [Apply and Webhooks](../apply-and-webhooks.md).

## Webhook Provider Resolution

### WebhookProvider

```go
type WebhookProvider struct {
    WebhookConfig entity.Entity   // The ValidatingWebhookConfiguration or MutatingWebhookConfiguration
    ServiceName   types.Name      // Resolved service name
    ServiceNs     types.Namespace // Resolved service namespace
    Workload      entity.Entity   // The Deployment/StatefulSet/DaemonSet backing the webhook service
}
```text

**Source file:** `core/commands/webhook.go`

Represents a resolved webhook configuration together with the workload that backs it. Used by `FilterWebhooksToDisable` (via `ResolveWebhookProviders`) to identify the backing workload for each webhook and check its cluster readiness. Previously also used by the bootstrap apply flow to scale up webhook providers individually.

### ResolveWebhookProviders

```go
func ResolveWebhookProviders(
    l log.Logger,
    webhookEntities entity.Entities,
    allEntities entity.Entities,
    key types.EntityKeyUnstructured,
) ([]WebhookProvider, error)
```

**Source file:** `core/commands/webhook.go`

Pure function that resolves webhook configurations to their backing workloads. Operates entirely on rendered entities — no cluster API calls are needed.

**Note:** Called by `FilterWebhooksToDisable` in the webhook-disable phase to resolve webhook providers and determine which webhooks need disabling. No longer called directly for individual scale-up — all workloads are scaled up uniformly via `ScaleUpWorkloads` in the dedicated scale-up phase.

```text
For each webhook config entity:
  │
  ├── Parse webhooks[].clientConfig.service entries
  │   (may have multiple webhooks, each potentially different service)
  │
  ├── For each unique service (name + namespace):
  │   │
  │   ├── Find v1/Service entity in allEntities by name + namespace
  │   │   If not found: log warning, skip (service may be external)
  │   │
  │   ├── Read spec.selector from Service
  │   │   If no selector: log warning, skip
  │   │
  │   └── Find workload in allEntities:
  │       - Same namespace as service
  │       - GVK is Deployment, StatefulSet, or DaemonSet
  │       - Pod template labels (spec.template.metadata.labels) contain ALL selector labels
  │       If not found: log warning, skip
  │       If multiple match: prefer Deployment over StatefulSet/DaemonSet, use first match, log warning
  │       If found: create WebhookProvider entry
  │
  └── Deduplicate: if multiple webhooks in same config point to same workload,
      only one WebhookProvider entry
```text

**Error handling**: Missing services or workloads are logged as warnings, NOT errors. The bootstrap should continue even if some webhook providers cannot be resolved (they might be external services or services from different clusters).

#### Unit Tests (ResolveWebhookProviders)

1. `ValidatingWebhookConfiguration` with one webhook → resolves to one `WebhookProvider`
2. `ValidatingWebhookConfiguration` with multiple webhooks pointing to same service → one `WebhookProvider` (deduplicated)
3. `MutatingWebhookConfiguration` → also resolves correctly
4. Webhook pointing to non-existent service → warning logged, no `WebhookProvider` returned
5. Service without selector → warning logged, skipped
6. Service with selector but no matching workload → warning logged, skipped
7. Multiple webhook configs with different providers → multiple `WebhookProvider` entries
8. No webhook entities → empty result
9. Webhook with URL-based `clientConfig` (no service) → skipped (only service-based webhooks are resolved)
10. Two Deployments matching same Service selector → first match used, warning logged

### WebhookRule

```go
type WebhookRule struct {
    ApiGroups  []string
    Resources  []string
    Operations []string
}
```

**Source file:** `core/commands/webhook.go`

Represents a single admission rule from a webhook configuration's `.webhooks[].rules[]` entry. Each rule specifies which API groups, resources, and operations the webhook intercepts.

### ExtractWebhookRules

```go
func ExtractWebhookRules(e entity.Entity, key types.EntityKeyUnstructured) []WebhookRule
```text

**Source file:** `core/commands/webhook.go`

Parses `.webhooks[].rules[]` from a webhook configuration entity's Unstructured data. Returns all rules across all webhook entries in the configuration. If the entity has no Unstructured data or no webhooks/rules, returns nil.

### WebhookMatchesEntities

```go
func WebhookMatchesEntities(rules []WebhookRule, entities entity.Entities) (bool, error)
```

**Source file:** `core/commands/webhook.go`

Checks whether any of the given webhook rules intercept any of the given entities. Returns true if at least one rule matches at least one entity.

Matching logic:

- Only rules with `CREATE` or `UPDATE` operations are considered (Apply uses SSA which triggers CREATE/UPDATE)
- `entity.Group()` is matched against `rule.ApiGroups` (wildcard `"*"` matches all groups)
- `entity.Resource()` is matched against `rule.Resources` (wildcards `"*"` and `"*/*"` match all resources)
- If `rule.Operations` is empty, the rule is treated as matching all operations

### FilterWebhooksToDisable

```go
func FilterWebhooksToDisable(
    l log.Logger,
    webhookEntities entity.Entities,
    nonWebhookEntities entity.Entities,
    allEntities entity.Entities,
    key types.EntityKeyUnstructured,
    isProviderReady func(provider WebhookProvider) (bool, error),
) (toDisable entity.Entities, toKeep entity.Entities, err error)
```text

**Source file:** `core/commands/webhook.go`

Determines which webhook configurations should be disabled before applying resources. For each webhook entity:

1. Extract rules via `ExtractWebhookRules`
2. Check if rules match any non-webhook entity via `WebhookMatchesEntities`
3. If no match → toKeep (webhook does not intercept applied resources)
4. If match → resolve backing workload via `ResolveWebhookProviders`
5. Check if provider is ready via `isProviderReady` callback
6. If ready → toKeep (webhook provider is running, no need to disable)
7. If not ready → toDisable (webhook would block resource creation)

URL-based webhooks (clientConfig.url instead of service) are treated as ready (external services not under our control). If no backing workload is found, the webhook is added to toDisable (defensive: better to disable than to block the apply).

The `isProviderReady` callback is injected to facilitate unit testing.

#### Unit Tests (FilterWebhooksToDisable)

1. Webhook ready + rules match applied resources → toKeep
2. Webhook not ready + rules match applied resources → toDisable
3. Webhook rules do not match applied resources → toKeep
4. No backing workload found → toDisable
5. URL-based webhook → toKeep (treated as ready)
6. Empty webhook entities → both empty
7. Multiple webhooks, mixed readiness → correct split
