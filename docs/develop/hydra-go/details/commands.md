# Business Commands Detail Index

This index keeps the historical `details/commands.md` entry point stable while routing detailed command architecture to focused subfiles.

## Subfiles

- [commands/rendering-and-listing.md](commands/rendering-and-listing.md) — Rendering, scope information, cluster listing, and namespace handling.
- [commands/apply-and-webhooks.md](commands/apply-and-webhooks.md) — Apply phases, helper functions, webhook lifecycle handling, and phase logging.
- [commands/deletion-and-topology.md](commands/deletion-and-topology.md) — Uninstall selection, deletion behavior, topology execution, and scale flows.
- [commands/backup-and-restore.md](commands/backup-and-restore.md) — Per-app secret backup, restore, diff, and apply-integrated backup behavior.
- [commands/bootstrap.md](commands/bootstrap.md) — Bootstrap-only SOPS conversion and sync policy details.
- [commands/shared-command-mechanics.md](commands/shared-command-mechanics.md) — Shared mechanics, wildcard resolution, auxiliary command flows, and common types.

## Compatibility Stubs

## Overview

Moved to [commands/shared-command-mechanics.md](commands/shared-command-mechanics.md).

## Rendering Commands

Moved to [commands/rendering-and-listing.md](commands/rendering-and-listing.md).

## Listing Commands

Moved to [commands/rendering-and-listing.md](commands/rendering-and-listing.md).

## Uninstall via Ref Tags

Moved to [commands/deletion-and-topology.md](commands/deletion-and-topology.md).

## Selection / Marking Commands

Moved to [commands/deletion-and-topology.md](commands/deletion-and-topology.md).

## Finalizer Removal Commands

Moved to [commands/deletion-and-topology.md](commands/deletion-and-topology.md).

## Deletion Commands

Moved to [commands/deletion-and-topology.md](commands/deletion-and-topology.md).

## Topological Execution

Moved to [commands/deletion-and-topology.md](commands/deletion-and-topology.md).

## ServerSideApply Annotation Support

Moved to [commands/apply-and-webhooks.md](commands/apply-and-webhooks.md).

## Apply Helper Functions

Most helper functions moved to [commands/apply-and-webhooks.md](commands/apply-and-webhooks.md). The bootstrap-specific `PreventSyncWindows` subsection moved to [commands/bootstrap.md](commands/bootstrap.md).

## Namespace Commands

Moved to [commands/rendering-and-listing.md](commands/rendering-and-listing.md).

## Entity Utility Functions

Moved to [commands/deletion-and-topology.md](commands/deletion-and-topology.md).

## Webhook Provider Resolution

Moved to [commands/apply-and-webhooks.md](commands/apply-and-webhooks.md).

## Per-App Secret Backup System

Moved to [commands/backup-and-restore.md](commands/backup-and-restore.md).

## API Version Normalization

Moved to [commands/shared-command-mechanics.md](commands/shared-command-mechanics.md).

## Wildcard App ID Matching

Moved to [commands/shared-command-mechanics.md](commands/shared-command-mechanics.md).

## Data Flow (Automatically Numbered Standard Apply)

Moved to [commands/apply-and-webhooks.md](commands/apply-and-webhooks.md#data-flow-automatically-numbered-standard-apply).

## Data Flow (Automatically Numbered Bootstrap Apply)

Moved to [commands/apply-and-webhooks.md](commands/apply-and-webhooks.md#data-flow-automatically-numbered-bootstrap-apply).

## Phase Logging

Moved to [commands/apply-and-webhooks.md](commands/apply-and-webhooks.md#phase-logging).

## Helper Function: `findChangedEntities`

Moved to [commands/apply-and-webhooks.md](commands/apply-and-webhooks.md#helper-function-findchangedentities).

## Data Flow (Uninstall)

Moved to [commands/deletion-and-topology.md](commands/deletion-and-topology.md#data-flow-uninstall).

## Data Flow (Cluster Scale)

Moved to [commands/deletion-and-topology.md](commands/deletion-and-topology.md#data-flow-cluster-scale).

## Bootstrap (--bootstrap flag)

Split across [commands/bootstrap.md](commands/bootstrap.md) for SOPS conversion and AppProject sync policy, and [commands/apply-and-webhooks.md](commands/apply-and-webhooks.md) for the shared apply/webhook phase plan.

## Data Flow (View / Dependency Graph)

Moved to [commands/shared-command-mechanics.md](commands/shared-command-mechanics.md#data-flow-view--dependency-graph).

## Error Types

Moved to [commands/shared-command-mechanics.md](commands/shared-command-mechanics.md#error-types).

## Types

Moved to [commands/shared-command-mechanics.md](commands/shared-command-mechanics.md#types).

## Test Commands

Moved to [commands/shared-command-mechanics.md](commands/shared-command-mechanics.md#test-commands).

## Review Commands

Moved to [commands/shared-command-mechanics.md](commands/shared-command-mechanics.md#review-commands).

## Source Files Summary

Moved to [commands/shared-command-mechanics.md](commands/shared-command-mechanics.md#source-files-summary).
