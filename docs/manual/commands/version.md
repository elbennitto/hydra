# hydra version

Print the Hydra CLI version.

## Synopsis

```bash
hydra version
```

## Description

Prints a single line with the build version and Git SHA, for example `hydra v1.2.3 a1b2c3d4`. Local development builds without release metadata report `hydra dev`.

Hydra skips the usual stderr welcome line for this command so stdout stays a single line suitable for scripts.

## Example

```bash
$ hydra version
hydra dev
```

## Tutorials

- [Install Hydra With Homebrew](../tutorials/installation/install-with-homebrew.md) — uses `hydra version` to verify that the installed CLI works
