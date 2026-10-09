# Installation

[Back to the README](../README.md#installation)

[curl installer](#curl-installer) · [Oh My Zsh](#oh-my-zsh) ·
[zinit](#zinit) · [Manual plugin](#manual-plugin-installation) ·
[Source build](#build-from-source) · [Updating](#updating) · [Uninstall](#uninstall)

For Homebrew and standard shell setup, follow the
[README installation steps](../README.md#installation). These instructions
cover other ways to install or load deja.

## curl installer

The installer supports macOS and Linux, including WSL on Windows, on `amd64`
and `arm64`. It downloads
the latest release, verifies the archive checksum, and installs the binary to
`~/.local/bin/deja` by default.

[Read the installer](https://github.com/Giammarco-Ferranti/deja/blob/main/install.sh)
before running it:

```sh
curl -fsSL https://raw.githubusercontent.com/Giammarco-Ferranti/deja/main/install.sh | sh
```

If your login shell is zsh, the installer imports history and adds the cached
integration block to `.zshrc`, using `$ZDOTDIR` if set. It skips adding the block
when it finds `deja/init.zsh` in that file. Restart your shell afterward:

```sh
exec zsh
```

If the installer does not detect zsh, follow its printed setup instructions.
Ensure the installation directory is on `$PATH`. To install elsewhere, set
`INSTALL_DIR` for the installer process, for example:

```sh
curl -fsSL https://raw.githubusercontent.com/Giammarco-Ferranti/deja/main/install.sh \
  | INSTALL_DIR="$HOME/bin" sh
```

## Oh My Zsh

Install the binary first:

```sh
brew install deja
```

Clone the plugin into [Oh My Zsh](https://ohmyz.sh)'s custom plugin directory:

```sh
git clone https://github.com/Giammarco-Ferranti/deja \
  "${ZSH_CUSTOM:-$HOME/.oh-my-zsh/custom}/plugins/deja"
```

Add `deja` to the existing `plugins=(...)` list in your `.zshrc`. Remove
`zsh-autosuggestions` from that list. The deja plugin loads the integration,
so remove any separate deja `source` or `eval` activation block.

Import history once, then restart:

```sh
deja import
exec zsh
```

You can use the curl installer for the binary instead. Remove its activation
block before enabling the plugin.

## zinit

Install the binary with Homebrew or the curl installer. Then add this to
your `.zshrc` for [zinit](https://github.com/zdharma-continuum/zinit):

```zsh
zinit ice wait"0" lucid depth=1 pick"deja.plugin.zsh"
zinit light Giammarco-Ferranti/deja
```

Remove any separate deja activation block and disable `zsh-autosuggestions`.
Run `deja import` once, then `exec zsh` to load the plugin.

## Manual plugin installation

To use the Oh My Zsh plugin without cloning the repository, download its file:

```sh
mkdir -p "${ZSH_CUSTOM:-$HOME/.oh-my-zsh/custom}/plugins/deja"
curl -fsSL https://raw.githubusercontent.com/Giammarco-Ferranti/deja/main/deja.plugin.zsh \
  -o "${ZSH_CUSTOM:-$HOME/.oh-my-zsh/custom}/plugins/deja/deja.plugin.zsh"
```

Then follow the [Oh My Zsh activation instructions](#oh-my-zsh). The binary
must be installed separately and available on `$PATH`.

For a shell without a plugin manager, use the cached integration block in the
[README](../README.md#installation).

## Build from source

Use the Go version specified in [go.mod](https://github.com/Giammarco-Ferranti/deja/blob/main/go.mod)
or a newer compatible version. SQLite requires CGO and a C compiler.

```sh
git clone https://github.com/Giammarco-Ferranti/deja.git
cd deja
make build
```

The release-style binary is `./bin/deja`. Put it on `$PATH`, then follow the
[history import and shell setup steps](../README.md#installation).

For development checks and debug builds, see [CONTRIBUTING.md](../CONTRIBUTING.md).

## Updating

For a Homebrew installation:

```sh
brew upgrade deja
```

For a curl installation, rerun the installer. For a source build, rebuild the
binary from the version you want to use.

Restart your shell to load the integration, then replace the old daemon:

```sh
exec zsh
```

```sh
deja daemon --restart
```

The daemon command runs in the foreground; use another terminal to keep
working. See [daemon recovery](troubleshooting.md#daemon-recovery) for the
alternative of stopping the daemon and letting the integration start it.

## Uninstall

1. Remove deja's activation block from `.zshrc`, or remove `deja` from your
   plugin manager's configuration. Remove its plugin directory if you installed one.
2. Stop the daemon:

   ```sh
   pkill -f 'deja daemon'
   ```

3. Delete deja's local data if you no longer want to keep the recorded history:

   ```sh
   rm -rf "$HOME/.local/share/deja"
   ```

   This deletes the database, saved settings, and generated integration script.
   If deja used a runtime socket fallback, remove its socket and pidfile from
   that directory after the daemon stops; see [data locations](privacy.md#data-locations).

4. Remove the binary. For Homebrew:

   ```sh
   brew uninstall deja
   ```

   For the curl installer's default location:

   ```sh
   rm "$HOME/.local/bin/deja"
   ```

   If you chose another installation directory, remove the binary there instead.
5. Restart your shell with `exec zsh`.
