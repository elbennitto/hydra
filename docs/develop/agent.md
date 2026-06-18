# Hydra Developer Documentation

Architecture documentation for hydra-go and hydra-ui development.

## How to use this documentation

1. Read the relevant section's `agent.md` to find topics related to your task.
2. Read the topic overview file for a summary of key concepts.
3. Only load files from `details/` when you need full implementation specifics.

## Sections

| Section                       | Description                                                                                                                                            |
| ----------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| [Shared](shared/agent.md)     | Concepts used by both backend and frontend: context hierarchy, entity grouping, values composition, secrets, hydra.yaml format, cluster dump structure |
| [Hydra Go](hydra-go/agent.md) | CLI backend: module structure, rendering pipeline, commands, entity processing, Helm integration, Git operations, CEL expressions, references, diff    |
| [Hydra UI](hydra-ui/agent.md) | Web frontend: state management, graph layout, navigation, sidebar, filters, RBAC display, components, values preview, fulltext search                  |
