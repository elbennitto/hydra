# hydra ci run verify

Validate the provenance signatures of charts that were already published to the OCI registry.

## Synopsis

```text
hydra ci run verify [flags] <config-path>
```

## Description

`hydra ci run verify` resolves charts the same way as `hydra ci run publish`, downloads the remote chart archive and signature artifacts from `ci.registry`, and verifies every configured signing mechanism (Helm provenance and/or Cosign) using the keys in `.hydra-ci.yaml`.

Hydra does not package charts again and does not validate chart contents beyond signature verification.

After the run, Hydra prints a per-chart result list with successful validations and failures, including reasons such as:

- chart download failed
- provenance signature missing
- provenance signature invalid
- signer not listed in `ci.sign.helm.validKeys` or `ci.sign.cosign.validKeys`

`hydra ci run validate` is a hidden deprecated alias for `verify`.

## Flags

| Flag | Description |
| ---- | ----------- |
| `--build-tag <build-...>` | Resolve charts from the specified build tag instead of `HEAD`. |
| `--force-run` | Resolve charts even when `HEAD` is not at the expected release or build commit. |
| `--chart <path>` | Validate only the selected chart. Repeatable. Accepts `<group>/<app>/<env>` or a repo-relative chart path. |

Persistent `hydra ci` flags (`--dry-run`, `--local`, `--target-branch`, `--promote-to`) apply to `run` subcommands as documented in [hydra ci](README.md).

## Examples

```bash
hydra ci run verify .hydra-ci.yaml
hydra ci run verify --build-tag build-202601011200 .hydra-ci.yaml
hydra ci run verify --chart demo/service-ui/dev .hydra-ci.yaml
```

## See Also

- [hydra ci](README.md)
- [hydra ci run publish](publish.md)
