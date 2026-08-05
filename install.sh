#!/usr/bin/env sh
set -eu

REPO="Giammarco-Ferranti/deja"
BINARY="deja"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

err() {
  printf 'install: %s\n' "$*" >&2
  exit 1
}

detect_os() {
  case "$(uname -s)" in
    Darwin) printf 'darwin' ;;
    Linux)  printf 'linux'  ;;
    *) err "unsupported OS: $(uname -s)" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64)   printf 'amd64' ;;
    arm64|aarch64)  printf 'arm64' ;;
    *) err "unsupported arch: $(uname -m)" ;;
  esac
}

command -v curl >/dev/null 2>&1 || err "curl is required"
command -v tar  >/dev/null 2>&1 || err "tar is required"

OS=$(detect_os)
ARCH=$(detect_arch)

printf 'Resolving latest release...\n'
TAG=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
  | grep '"tag_name":' \
  | head -n1 \
  | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')
[ -n "$TAG" ] || err "could not resolve latest release tag"
VERSION=${TAG#v}

ARCHIVE="${BINARY}_${VERSION}_${OS}_${ARCH}.tar.gz"
BASE_URL="https://github.com/$REPO/releases/download/$TAG"

TMP=$(mktemp -d 2>/dev/null || mktemp -d -t deja)
trap 'rm -rf "$TMP"' EXIT

printf 'Downloading %s...\n' "$ARCHIVE"
curl -fsSL -o "$TMP/$ARCHIVE"      "$BASE_URL/$ARCHIVE"
curl -fsSL -o "$TMP/checksums.txt" "$BASE_URL/checksums.txt"

printf 'Verifying checksum...\n'
if command -v shasum >/dev/null 2>&1; then
  (cd "$TMP" && grep " $ARCHIVE\$" checksums.txt | shasum -a 256 -c -) \
    || err "checksum verification failed"
elif command -v sha256sum >/dev/null 2>&1; then
  (cd "$TMP" && grep " $ARCHIVE\$" checksums.txt | sha256sum -c -) \
    || err "checksum verification failed"
else
  err "neither shasum nor sha256sum is available"
fi

tar -xzf "$TMP/$ARCHIVE" -C "$TMP"
[ -f "$TMP/$BINARY" ] || err "archive did not contain $BINARY"

mkdir -p "$INSTALL_DIR"
mv "$TMP/$BINARY" "$INSTALL_DIR/$BINARY"
chmod +x "$INSTALL_DIR/$BINARY"
printf 'Installed %s to %s\n' "$BINARY" "$INSTALL_DIR/$BINARY"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) printf 'Note: %s is not on your PATH. Add it to your shell profile.\n' "$INSTALL_DIR" ;;
esac

is_zsh=0
case "${SHELL:-}" in *zsh) is_zsh=1 ;; esac
[ -n "${ZSH_VERSION:-}" ] && is_zsh=1

if [ "$is_zsh" -eq 1 ]; then
  ZSHRC="${ZDOTDIR:-$HOME}/.zshrc"
  "$INSTALL_DIR/$BINARY" import || printf 'Note: deja import failed; you can rerun it manually.\n'
  if ! grep -qF 'deja/init.zsh' "$ZSHRC" 2>/dev/null; then
    # Source the cached integration rather than `eval "$(deja init zsh)"`: that
    # eval costs a full binary launch on every shell to regenerate a file that
    # is almost always identical. The eval is only the first-run bootstrap.
    printf '\nif [[ -r "$HOME/.local/share/deja/init.zsh" ]]; then\n  source "$HOME/.local/share/deja/init.zsh"\nelse\n  eval "$(deja init zsh)"\nfi\n' >> "$ZSHRC"
    printf 'Added deja init to %s\n' "$ZSHRC"
  else
    printf 'deja init already present in %s\n' "$ZSHRC"
  fi
  printf 'Done. Restart your shell or run: exec zsh\n'
else
  printf 'Done. To activate deja in zsh, add this to your ~/.zshrc:\n'
  printf '  if [[ -r "$HOME/.local/share/deja/init.zsh" ]]; then\n'
  printf '    source "$HOME/.local/share/deja/init.zsh"\n'
  printf '  else\n'
  printf '    eval "$(deja init zsh)"\n'
  printf '  fi\n'
  printf 'Then run: deja import\n'
fi
