# Install Hydra With Homebrew

This tutorial walks through installing Hydra with Homebrew.

## Related CLI Pages

- [`hydra version`](../../commands/version.md) — verify that the installed CLI works

Tap the Hydra Homebrew repository first:

```bash
brew tap hydra-gitops/homebrew-tap https://github.com/hydra-gitops/homebrew-tap
```

macOS recommended:

```bash
brew trust --formula hydra-gitops/tap/hydra
brew install hydra-gitops/tap/hydra
```

Linux recommended:

```bash
brew trust --formula hydra-gitops/tap/hydra-bin
brew install hydra-gitops/tap/hydra-bin
```

To uninstall later:

```bash
brew uninstall hydra-gitops/homebrew-tap/hydra
# or
brew uninstall hydra-gitops/homebrew-tap/hydra-bin
brew untap hydra-gitops/tap
```

Hydra provides both a source formula (`hydra`) and a binary package (`hydra-bin`).
On Linux, `hydra-bin` is recommended and downloads the released binary from GitHub.
On Linux, self-compiling from source with `hydra-gitops/tap/hydra` also works.
On macOS, `hydra-gitops/tap/hydra` is recommended.

## Step 1: Check Whether `hydra` Is Already Installed

Start by checking whether `hydra` is available:

```bash
command -v hydra
```

## Step 2: Install Hydra With Homebrew

Use the command for your platform. If you did not already add the tap, run:

```bash
brew tap hydra-gitops/homebrew-tap https://github.com/hydra-gitops/homebrew-tap
```

Then install Hydra:

macOS recommended:

```bash
brew trust --formula hydra-gitops/tap/hydra
brew install hydra-gitops/tap/hydra
```

Linux recommended:

```bash
brew trust --formula hydra-gitops/tap/hydra-bin
brew install hydra-gitops/tap/hydra-bin
```

## Step 3: Check Again

After the installation, verify that `hydra` is now available:

```bash
command -v hydra
```

## Step 4: Verify The CLI

```bash
hydra version
```

## Demo Videos

=== "Linux"

	<div class="hydra-asciinema" data-cast-path="tutorials/installation/homebrew-linux.cast"></div>

=== "macOS"

	<div class="hydra-asciinema" data-cast-path="tutorials/installation/homebrew-macos.cast"></div>
