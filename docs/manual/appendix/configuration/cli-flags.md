# Configuration: CLI Flags

Global flags available on all Hydra commands (from `hydra --help`).

## Global Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--verbose` | `-v` | Show debug-level log messages |
| `--quiet` | `-q` | Only show warnings and errors |
| `--color` | | Force colored output for commands that support colorized stdout |
| `--no-color` | | Disable colored output for commands that support colorized stdout |
| `--color-mode` | | Set color mode for colorized stdout (`auto`, `always`, or `never`) |
| `--color-log` | | Force colored log output (default: auto-detect from terminal) |
| `--no-color-log` | | Disable colored log output |
| `--no-timestamps` | | Omit timestamps from log messages |
| `--json-log` | | Output logs in JSON format (useful for log aggregation) |
| `--no-progress` | | Disable terminal progress bars on supported commands |
| `--less` | | Pipe combined stdout and stderr through `$PAGER` (default: `less -SR +G`); the child enables `--color` and `--color-log` unless you disable them |
| `--help` | `-h` | Show help |

`--color`, `--no-color`, and `--color-mode` are mutually exclusive. `--verbose` and `--quiet` are mutually exclusive. `--no-color-log`, `--color-log`, and `--json-log` are mutually exclusive.

At the default log level, Hydra prints a short welcome line with the CLI version to stderr after logging starts. That line is omitted for `hydra version`, which prints a single `hydra <version> <sha>` line on stdout.

## Common Command Flags

Many `hydra local` and `hydra gitops` commands also accept:

| Flag | Description |
|------|-------------|
| `--hydra-context <path>` | Path to the Hydra context directory (see [HYDRA_CONTEXT](hydra-context.md)) |
| `--exclude-app <pattern>` | Exclude applications matching a glob (repeatable) |
| `--helm-network-mode <mode>` | Chart dependency resolution: `online`, `local`, `offline`, or `error` |
| `--include` / `--exclude` | [CEL resource filters](../commands/README.md#cel-resource-filters) on rendered resources |

`hydra gitops` commands additionally support persistent REST client tuning:

| Flag | Description |
|------|-------------|
| `--qps` | Kubernetes REST client QPS limit (`0` = client-go default; negative disables client-side throttling) |
| `--api-burst` | Burst size when `--qps` is positive (`0` = client-go default) |

## Examples

```bash
# Use a different Hydra context directory
hydra --hydra-context /path/to/gitops local template 'prod.**'

# Debug logging
hydra -v gitops diff prod.infra.*

# Quiet scripting
hydra -q local list prod | wc -l

# Page long output
hydra --less gitops dump prod
```

## Environment Variables

| Variable | Effect |
|----------|--------|
| `HYDRA_CONTEXT` | Default Hydra context directory when `--hydra-context` is omitted |
| `NO_COLOR` | Disables colored output when set (standard convention) |

## See Also

- [HYDRA_CONTEXT](hydra-context.md)
- [User kubeconfig mapping](user-kubeconfig-mapping.md)
- [Command reference](../commands/README.md)
