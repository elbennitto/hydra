# Install Hydra With Homebrew

This tutorial walks through installing Hydra with Homebrew.

## Related CLI Pages

- [`hydra version`](../../commands/version.md) — verify that the installed CLI works

## Step 1: Check Whether `hydra` Is Already Installed

Start by checking whether `hydra` is available:

```bash
command -v hydra
```

## Step 2: Install Hydra With Homebrew

Add the Hydra tap:

```bash
brew tap hydra-gitops/homebrew-tap https://github.com/hydra-gitops/homebrew-tap
```

Install the package for your platform.

macOS recommended (source formula):

```bash
brew trust --formula hydra-gitops/tap/hydra
brew install hydra-gitops/tap/hydra
```

Linux recommended (prebuilt binary cask):

```bash
brew trust --cask hydra-gitops/tap/hydra-bin
brew install --cask hydra-gitops/tap/hydra-bin
```

Hydra provides both a source formula (`hydra`) and a binary cask (`hydra-bin`). On Linux, `hydra-bin` is recommended and downloads the released binary from GitHub. On macOS, `hydra-gitops/tap/hydra` is recommended. On Linux, self-compiling from source with `hydra-gitops/tap/hydra` also works.

## Step 3: Check Again

After the installation, verify that `hydra` is now available:

```bash
command -v hydra
```

## Step 4: Verify The CLI

```bash
hydra version
```

## Uninstall

```bash
brew uninstall hydra-gitops/tap/hydra
# or
brew uninstall --cask hydra-gitops/tap/hydra-bin
brew untap hydra-gitops/tap
```

## Demo Videos

=== "Linux"

	<div class="hydra-asciinema" data-cast-path="tutorials/installation/homebrew-linux.cast"></div>

=== "macOS"

	<div class="hydra-asciinema" data-cast-path="tutorials/installation/homebrew-macos.cast"></div>
