# hydra yq

Run the embedded [yq](https://github.com/mikefarah/yq) CLI (mikefarah/yq v4) with the same interface as the standalone `yq` binary.

## Synopsis

```bash
hydra yq [yq arguments and flags]
```

## Description

`hydra yq` registers upstream yq’s Cobra command tree under Hydra (`hydra-go/cli/cmd/delegated_cli.go` via `github.com/mikefarah/yq/v4/cmd`). All subcommands, flags, and default behavior match the embedded yq release (including implicit `eval` when the first argument is not a known subcommand).

Hydra skips the usual stderr welcome line for `hydra yq` (and other delegated tool CLIs) so stdout stays suitable for pipes and scripts.

Hydra uses the same yq library internally for `global.hydra.templatePatches`, diff-ignore rules, and YAML coloring; `hydra yq` is for ad-hoc querying and editing YAML/JSON in pipelines without installing a separate `yq` binary.

For expressions, operators, input/output formats, and flags, use the official upstream documentation: [yq documentation](https://mikefarah.gitbook.io/yq/) (same CLI as `hydra yq`; only the command prefix differs).

## Examples

```bash
# Same as: yq '.metadata.name' file.yaml
hydra yq '.metadata.name' file.yaml

# Explicit eval subcommand
hydra yq eval '.spec.replicas' deployment.yaml

# Version of the embedded yq
hydra yq --version

# Pipe Hydra render output into yq
hydra local template prod.apps.my-service | hydra yq '.items[] | select(.kind == "Deployment")'
```

## See also

- [yq documentation](https://mikefarah.gitbook.io/yq/) — upstream CLI reference (expressions, flags, formats)
- [Delegated tool CLIs](../README.md#delegated-tool-clis) in the command reference
- [hydra local find](../local/find.md) — CEL projection on rendered resources (Hydra-native alternative to `template | yq`)
