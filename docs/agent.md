# Hydra Documentation

Hydra is a GitOps deployment tool built on ArgoCD and Helm. It consists of a Go CLI backend (`hydra-go`) that renders Helm charts, processes Kubernetes entities, and produces a `.hydra.yaml` dependency graph, and a React/TypeScript web frontend (`hydra-ui`) that visualizes the graph.

## Sections

| Section                            | Description                                                      |
| ---------------------------------- | ---------------------------------------------------------------- |
| [Developer Docs](develop/agent.md) | Architecture documentation for hydra-go and hydra-ui development |
| [User Manual](manual/README.md)    | CLI reference for Kubernetes administrators                      |

## Developer Documentation

For architecture and implementation details, see [develop/agent.md](develop/agent.md).

## User Manual

For CLI command reference from a Kubernetes administrator perspective, see [manual/README.md](manual/README.md).
