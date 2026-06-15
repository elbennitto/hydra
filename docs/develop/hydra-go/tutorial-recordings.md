# Tutorial Recordings

Hydra records tutorial casts from declarative YAML specs with:

```bash
hydra record file <file>...
```

Specs are discovered under `docs/asciinema/tutorials/`.
When the shell expands a glob such as `docs/asciinema/tutorials/first-context/*.yaml`,
Hydra records each matched YAML spec in order. `--output` is only valid when exactly
one input file is recorded.

## Runtime Model

- Every recording runs in a fresh temporary directory.
- A virtual root path `/home/hydra` is shown in cast output.
- `cd` is allowed but must stay inside the recording root.
- Commands are executed through shell parsing (`bash -lc`) so pipes, redirects, and variable expansion are available.
- `run` executes via shell parsing (`bash`), captures `stdout` and `stderr`, and can optionally mirror output into the cast (`output: true|false`).

## Step DSL

Each item in `steps` must define exactly one of these forms:

- `type: prompt`
- `type: newline`
- `sleep: <seconds>`
- `write: <text>`
- `run: <shell command>`
- `cd: <path>`
- `env: <entry-list>`
- `color: reset | { fg: <name>, bg: <name>, bold: <bool>, reset: <bool> }`
- `assert: { stdout: <cel>, stderr: <cel>, cast: <cel> }`

### `write`

- Writes text directly to the cast.
- Optional: `speed` for character-by-character typing.

### `run`

- Executes a shell command.
- Optional:
	- `input` (default `true`): show or hide the typed command in the cast.
	- `output` (default `true`): show or hide command output in the cast.
	- `expectedExitCode` (default `0`).
	- `speed`: typing speed for visible command input.

### `color`

- `color: reset` emits an ANSI reset.
- Object form supports foreground/background and bold styling.
- Typical prompt flow is `type: prompt` -> command text -> `color: reset` -> `type: newline`.

### `cd`

- Changes working directory for subsequent `run`/`env.exec` commands.
- Relative paths such as `..` are supported.
- Leaving the recording root is rejected.

### `env`

`env` requires a non-empty list:

```yaml
- env:
    - name: A
      value: alpha
    - name: B
      cel: env.A + "-beta"
    - name: C
      exec: printf '%s' "$PWD"
```

Rules per env entry:

- `name` is required.
- Exactly one source is required:
	- `value`: static string.
	- `cel`: CEL expression. Variable `env` contains all env vars set so far.
	- `exec`: shell command; command `stdout` becomes the env value.

### `assert`

- Assertions evaluate against accumulated recording streams.
- Supported fields:
	- `stdout`: raw accumulated command stdout stream.
	- `stderr`: raw accumulated command stderr stream.
	- `cast`: raw merged stream (`stdout + stderr`).
- CEL variables:
	- `history`: selected stream text for the active assert field (legacy alias).
	- `stdout`: full raw stdout stream.
	- `stderr`: full raw stderr stream.
	- `cast`: full raw merged stream (`stdout + stderr`).
	- `lines`: selected stream split into lines.
	- `env`: currently defined env vars.
	- `plain(text)`: strips ANSI/shell escape sequences before matching.

`clear` is not supported anymore.

## Example

```yaml
name: Installation - Homebrew (Simulated)
steps:
	- type: prompt
	- write: command -v hydra
		speed: 0.2
	- color: reset
	- type: newline
	- sleep: 1
	- run: brew tap hydra-gitops/homebrew-tap https://github.com/hydra-gitops/homebrew-tap.git
	- run: brew install hydra
	- run: hydra version
	- assert:
			cast: plain(cast).contains("hydra")
```
