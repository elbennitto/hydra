# CEL

Common Expression Language (CEL) in Hydra — used for filtering, matching, and extracting dependencies.

## Contents

- [Variables](variables.md) — All available variables by context
- [Functions](functions.md) — refBuilder(), id(), clusterEntities(), and more
- [Predicates](predicates.md) — --include/--exclude filter syntax
- [Examples](examples.md) — Practical CEL expression cookbook

## Overview

CEL (Common Expression Language) is a lightweight expression language used throughout Hydra for:

| Usage | Where |
|-------|-------|
| Ref-parser predicates | Select which resources a parser applies to |
| Ref-parser pick rules | Extract dependency endpoints |
| CLI filters | `--include` / `--exclude` flags |
| Preset predicates | Match infrastructure resources |
| Value predicates | templatePatches, clones, diff, ready, scale |

## Quick Start

CEL expressions evaluate to `true`/`false` (predicates) or lists (ref extraction).

### Simple Predicates

```cel
kind == "Deployment"
ns == "kube-system"
name.startsWith("coredns")
gvk == "apps/v1/Deployment" && ns == "default"
```

### Combining Conditions

```cel
kind == "Secret" && (ns == "default" || ns == "kube-system")
!(kind == "Event")
namespaced && !appOwned
```

### Accessing Fields

```cel
has(entity.metadata.annotations) && entity.metadata.annotations["key"] == "value"
entity.spec.replicas > 1
```

## Prerequisites

- [Concepts: Dependency Graph](../concepts/dependency-graph.md) — Context for how CEL is used
- [Refs: Ref Parsers](../refs/ref-parsers.md) — Primary use of CEL in Hydra
