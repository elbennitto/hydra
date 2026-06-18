# Pipeline: Sync and Update

This file covers repository synchronization and unit-test-data refresh behavior.

Back to [Pipeline detail index](../pipeline.md).

## Pipeline: `sync`

### Purpose

Copies cluster configurations from all external cluster repositories into
the current repository and triggers the `update` pipeline to refresh
unit test data.

### Trigger

- Timer (periodic schedule)

### Steps

1. **Identify cluster repositories:** Determine which external cluster
   repositories have configurations to sync
2. **Copy configurations:** Copy cluster configs to target locations
   in the current repository
3. **Trigger update:** Start the `update` pipeline to regenerate
   unit test data from the synced configurations

---

## Pipeline: `update`

### Purpose

Renders all cluster configurations via Helm template and stores the
resulting manifests as unit test data. This makes changes directly
visible in MRs and ensures that all cluster impacts are reviewed
before merging.

### Trigger

- MR is created or updated (against `main`)
- Timer (periodic schedule)
- Triggered by the `sync` pipeline

### Self-Detection

The pipeline renders all clusters and compares the output against stored
test data. If differences exist, it commits the changes.

### Steps

1. **Render all clusters:** Run Helm template for all cluster configurations
2. **Compare:** Diff rendered output against stored unit test data
3. **Commit if changed:** Create auto-commit with refreshed test data

### Governance

To prevent infinite loops, the following rules apply:

1. The pipeline may produce at most **one** auto-commit per MR
2. Auto-commits only for unit test data (no `Chart.yaml`/`values.yaml` rewrite)
3. The auto-commit uses a fixed commit pattern, e.g.
   `update: refresh unit test data [auto]`
4. If a diff is detected on the next run, `update` fails and requires
   manual review/commit

### MR Integration

- `update` is configured as a **required check**
- The MR can only be merged once `update` passes, ensuring that there are
  no uncommitted changes and all cluster impacts are visible during review

---
