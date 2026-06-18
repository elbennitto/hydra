# Pipeline: Configuration

This file documents `.hydra-ci.yaml`, the interactive `hydra ci config` flow, and configuration path handling.

Back to [Pipeline detail index](../pipeline.md).

## Configuration: `.hydra-ci.yaml`

Pipeline behavior is configured via a single `.hydra-ci.yaml` file in the
Git repository root.

```yaml
# .hydra-ci.yaml
ci:
  # Directory containing root apps (each with <env>/ subdirs)
  rootAppsPath: "apps"

  # Environment names in promotion order
  environments:
    - dev
    - stage
    - prod

  # App groups with their base paths
  appGroups:
    - name: demo
      path: apps/demo
    - name: cluster-infra
      path: apps/cluster-infra
    - name: demo-infra
      path: apps/demo-infra
    - name: cicd
      path: apps/cicd
    - name: argocd
      path: apps/argocd

  # OCI registry for chart publishing
  registry: "oci://ghcr.io/example-org/helm-charts"

  promote:
    # Root apps are NOT promotable by default.
    # List root apps here to allow their promotion.
    promotableRootApps: []
    # Example:
    # promotableRootApps:
    #   - demo
    #   - cluster-infra

  teams:
    # MS Teams webhook URL (optional)
    webhookUrl: ""
    # Channel override per app group (optional)
    # channels:
    #   demo: "https://..."
    #   cluster-infra: "https://..."
```

### Key Configuration Fields

| Field                           | Type         | Description                                                  |
| ------------------------------- | ------------ | ------------------------------------------------------------ |
| `ci.rootAppsPath`               | `string`     | Directory containing root apps (each with `<env>/` subdirs)  |
| `ci.environments`               | `[]string`   | Ordered list of environments (promotion flows left to right) |
| `ci.appGroups`                  | `[]AppGroup` | Name-to-path mapping for app groups                          |
| `ci.registry`                   | `string`     | OCI registry URL for `helm push`                             |
| `ci.promote.promotableRootApps` | `[]string`   | Root apps that may be promoted (empty = none promotable)     |
| `ci.teams.webhookUrl`           | `string`     | Default MS Teams webhook for notifications                   |

---

### hydra ci config

#### Purpose

An interactive CLI command to create or edit a `.hydra-ci.yaml` configuration
file. Instead of writing YAML by hand, developers run `hydra ci config <path>`
and are guided through every field with sensible defaults and filesystem-based
auto-detection.

#### Usage

```bash
hydra ci config <path>
```

The `config` subcommand is registered on the `ci` parent command but does
**not** use `CiFlags` (`--dry-run` / `--local`). It only accepts a single
positional argument `<path>` — the target file to create or edit.

#### Path Validation {#path-validation}

The `config` subcommand resolves directory paths in `CiConfigInit`.
The same resolution logic is now also applied to all pipeline subcommands
via `newCiSubcommand` — see
[Config Path Resolution (Pipeline Subcommands)](modes.md#config-path-resolution-pipeline-subcommands).

| Condition                               | Behavior                                                 |
| --------------------------------------- | -------------------------------------------------------- |
| `<path>` does not exist, parent exists  | Create a new file at `<path>`                            |
| `<path>` does not exist, parent missing | Error: parent directory does not exist                   |
| `<path>` exists and is a regular file   | Load the file, pre-populate all prompts with its values  |
| `<path>` exists and is a directory      | Append `.hydra-ci.yaml` to the path, then create or load |

#### Loading Existing Files

When `<path>` points to an existing file, the command reads the raw bytes
with `os.ReadFile(path)` and unmarshals them with `yaml.Unmarshal` directly
into the `Config` struct. It deliberately **does not** use `LoadConfig(dir)`
(which appends `ConfigFileName` and runs full validation) nor `ParseConfig`
(which also validates). Skipping validation allows loading partial or
incomplete configs so the interactive flow can fill in the missing fields.

#### Interactive Flow

For each configuration field the command:

1. Shows the current value (loaded from file) or the default value
2. Asks the user whether they want to change it `[y/n]`
3. If yes, prompts for the new value
4. After a change, shows `[y/n/r]` — **r** reverts to the original value

The fields are presented in the following order:

| Field                           | Default                                    | Notes                                  |
| ------------------------------- | ------------------------------------------ | -------------------------------------- |
| `ci.rootAppsPath`               | `"apps"`                                   | Directory containing root apps         |
| `ci.environments`               | **auto-detected** from `rootAppsPath/*/*/` | Unique names at 3rd level, editable    |
| `ci.appGroups`                  | **auto-detected** from filesystem          | User confirms or edits (see below)     |
| `ci.registry`                   | _(none — must be provided)_                | OCI registry URL                       |
| `ci.promote.promotableRootApps` | **auto-detected**, user selects            | Interactive selection loop (see below) |
| `ci.teams.webhookUrl`           | `""` (empty, optional)                     | MS Teams webhook URL                   |
| `ci.teams.channels`             | `{}` (empty, optional)                     | Per-app-group channel overrides        |

#### Auto-Detection: App Groups

App groups are derived from the filesystem:

```text
1. Take rootAppsPath (e.g. "apps")
2. List directories at <rootAppsPath>/*/
   apps/demo/         → AppGroup{name: "demo",            path: "apps/demo"}
   apps/cluster-infra/ → AppGroup{name: "cluster-infra", path: "apps/cluster-infra"}
   apps/cicd/        → AppGroup{name: "cicd",           path: "apps/cicd"}
   ...
4. Present the list and let the user confirm or edit
```

`rootAppsPath` is used directly as the base directory for app group
discovery. No glob expansion is needed since it is a plain directory path.

#### Auto-Detection: Promotable Root Apps

After app groups are determined, root apps are detected:

```text
1. For each AppGroup:
   a. Scan <group.path>/ for app directories
   b. An app is a root app if its name is "root"
      (i.e. <group.path>/root/ exists)
   c. A root app with at least one environment subdirectory
      (dev/, stage/, prod/) qualifies as a candidate
2. Present all detected root apps to the user
3. Let the user select which ones are promotable (interactive loop)
   - Show: "demo/root — promote? [y/n]"
   - Selected root apps go into ci.promote.promotableRootApps
```

#### Output

The final `Config` struct is serialized to YAML via `yaml.Marshal` and
written to `<path>`. Comments from a previously loaded file are **not**
preserved — they are dropped during unmarshal and will not appear in the
newly written file. This keeps the implementation simple and avoids
`yaml.Node` manipulation or template-based serialization.

#### Architecture Layers

##### `core/ci/config_writer.go` (new file)

Contains pure functions with no terminal I/O, making them independently testable.

```go
func WriteConfig(path string, cfg *Config) error
func DetectAppGroups(baseDir string) ([]AppGroup, error)
func DetectEnvironments(rootAppsPath string) ([]string, error)
func DetectRootApps(appGroups []AppGroup) ([]string, error)
func DefaultConfig() *Config
func ValidateOutputPath(path string) error
```

| Function             | Responsibility                                                                         |
| -------------------- | -------------------------------------------------------------------------------------- |
| `WriteConfig`        | Serialize `Config` via `yaml.Marshal` and write to file (no comments)                  |
| `DetectAppGroups`    | Scan filesystem at `baseDir` for immediate subdirectories, return as AppGroups         |
| `DetectEnvironments` | Glob `rootAppsPath/*/*/` for subdirectory names at 3rd level, return unique sorted set |
| `DetectRootApps`     | Scan each app group for apps named `root` with environment subdirectories              |
| `DefaultConfig`      | Return a `Config` pre-filled with sensible defaults (see table above)                  |
| `ValidateOutputPath` | Check that parent dir exists and path is not a directory                               |

`DetectAppGroups` takes `rootAppsPath` directly as the base directory.

##### `cli/action/ci_config.go` (new file)

Orchestrates the interactive flow. Takes `io.Reader` and `io.Writer`
parameters for input/output so the flow can be tested with `bytes.Buffer`
or `strings.Reader` instead of a real terminal.

```go
func CiConfigInit(path string, in io.Reader, out io.Writer) error
```

Algorithm:

```text
1.  ValidateOutputPath(path)
2.  If file exists at path:
      data := os.ReadFile(path)
      yaml.Unmarshal(data, &cfg)   // raw unmarshal, no validation
    else:
      cfg := DefaultConfig()
3.  Prompt rootAppsPath (editable, with revert)
3b. If environments empty → DetectEnvironments(rootAppsPath/*/*/) → auto-fill
3c. Prompt environments (editable, with revert)
3d. Prompt remaining fields (registry, teams — editable, with revert)
    Each prompt: [y/n] initially, [y/n/r] after a change (r = revert)
4.  DetectAppGroups(cfg.CI.RootAppsPath)
    → present to user, let them confirm/edit
5.  DetectRootApps(cfg.AppGroups)
    → present list, let user select promotable ones in a loop
6.  WriteConfig(path, cfg)
```

##### `cli/cmd/ci.go` (modify)

Add the `config` subcommand to the `ci` parent command:

- Registered separately from `newCiSubcommand` (which adds `CiFlags`)
- Accepts exactly one positional argument `<path>`
- Does **not** have `--dry-run` or `--local` flags
- `CiCommandParams` gets a new field:
  `CiConfig func(path string, in io.Reader, out io.Writer) error`
- The command handler calls `params.CiConfig(path, os.Stdin, os.Stdout)`

#### Data Flow

```text
┌──────────────┐     ┌─────────────────────┐     ┌──────────────────┐
│ cli/cmd/ci.go│────▶│cli/action/ci_config │────▶│core/ci/          │
│              │     │                     │     │config_writer.go  │
│ parse <path> │     │ CiConfigInit(       │     │                  │
│ register cmd │     │   path, in, out)    │     │ ValidateOutputPath│
│              │     │                     │     │ DefaultConfig     │
│ params.      │     │ os.ReadFile +       │     │                   │
│  CiConfig()  │     │ yaml.Unmarshal      │     │ DetectAppGroups   │
│              │     │ (no validation)     │     │ DetectRootApps    │
│              │     │                     │     │ WriteConfig       │
└──────────────┘     └─────────────────────┘     └──────────────────┘
```

#### Unit Tests — `core/ci/config_writer_test.go`

| #   | Test                                    | Setup                                                               | Assertion                                                                 |
| --- | --------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------------- |
| 1   | `TestWriteConfig`                       | Create Config, write to temp file                                   | Read back file, parse YAML, verify all fields match original Config       |
| 2   | `TestWriteConfig_CreatesFile`           | Temp dir exists, file does not                                      | File is created, content is valid YAML                                    |
| 3   | `TestWriteConfig_ErrorParentNotExist`   | Path with non-existent parent directory                             | Returns error                                                             |
| 4   | `TestWriteConfig_Roundtrip`             | Write Config via `WriteConfig`, read back via `ParseConfig`         | Parsed Config equals the original (all fields survive the roundtrip)      |
| 5   | `TestDetectAppGroups`                   | Create temp dirs: `apps/demo/`, `apps/cicd/`, `apps/infra/`          | Returns 3 AppGroups with correct names and paths                          |
| 6   | `TestDetectAppGroups_EmptyDir`          | Create empty base directory                                         | Returns empty slice, no error                                             |
| 7   | `TestDetectEnvironments`                | Create `demo/service-ui/{dev,stage,prod}/`, `cicd/root/{dev,stage}/` | Returns `["dev", "prod", "stage"]` (sorted, unique at 3rd level)          |
| 8   | `TestDetectEnvironments_EmptyDir`       | Create empty base directory                                         | Returns empty slice, no error                                             |
| 9   | `TestDetectEnvironments_NonExistent`    | Non-existent path                                                   | Returns nil, no error                                                     |
| 10  | `TestDetectRootApps`                    | Create `apps/demo/root/dev/`, `apps/cicd/root/dev/`                  | Returns `["demo", "cicd"]`                                                 |
| 11  | `TestDefaultConfig`                     | Call `DefaultConfig()`                                              | `RootAppsPath == "apps"`, `Environments` empty (auto-detected at runtime) |
| 12  | `TestValidateOutputPath_NewFile`        | Temp dir exists, path points to non-existent file within it         | Returns nil (ok)                                                          |
| 13  | `TestValidateOutputPath_ExistingFile`   | Create a temp file                                                  | Returns nil (ok — file will be loaded and overwritten)                    |
| 14  | `TestValidateOutputPath_IsDirectory`    | Path points to an existing directory                                | Returns error indicating path is a directory                              |
| 15  | `TestValidateOutputPath_ParentNotExist` | Path under `/nonexistent/dir/.hydra-ci.yaml`                        | Returns error indicating parent does not exist                            |

#### Unit Tests — `cli/action/ci_config_test.go`

| #   | Test                                      | Setup                                                                                | Assertion                                                                                |
| --- | ----------------------------------------- | ------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------- |
| 1   | `TestCiConfigInit_AllDefaults`            | `in`: all `"n\n"` (accept defaults), path to new file in temp dir, registry provided | File created, content matches `DefaultConfig` with the provided registry                 |
| 2   | `TestCiConfigInit_ExistingFileRoundtrip`  | Write a valid `.hydra-ci.yaml` to temp dir, `in`: all `"n\n"` (no changes)           | File content unchanged after roundtrip (loaded → no edits → written back)                |
| 3   | `TestCiConfigInit_ModifyField`            | `in`: `"y\n"` for registry, then `"oci://new-registry\n"`, rest `"n\n"`              | Written file has updated registry, all other fields retain original values               |
| 4   | `TestCiConfigInit_InvalidInput`           | `in`: empty reader (simulates EOF / no input)                                        | Returns error (does not panic or write incomplete file)                                  |
| 5   | `TestCiConfigInit_PartialConfig`          | Existing file with only `ci.registry` set (no rootAppsPath, no environments)         | Loads without validation error, interactive flow fills in remaining fields with defaults |
| 6   | `TestCiConfigInit_RevertToOriginal`       | Existing file with registry set, change then press `r`                               | Registry reverts to original value, output contains `[y/n/r]`                            |
| 7   | `TestCiConfigInit_AutoDetectEnvironments` | Create `apps/<group>/<app>/{dev,stage,prod}/` dirs, new config                       | Environments auto-detected, output contains "Auto-detected environments"                 |

All tests inject `bytes.Buffer` / `strings.Reader` as `in` and
`bytes.Buffer` as `out`, confirming the flow works without a real terminal.

---
