#!/usr/bin/env bash
# install.sh - installs sbxgo from GitHub Releases
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/HenrikPoulsen/sbxgo/main/install.sh | bash
#   curl -fsSL https://raw.githubusercontent.com/HenrikPoulsen/sbxgo/main/install.sh | bash -s v0.3.0
#   curl -fsSL https://raw.githubusercontent.com/HenrikPoulsen/sbxgo/main/install.sh | SBXGO_VERSION=v0.3.0 bash
set -euo pipefail

REPO="HenrikPoulsen/sbxgo"
BINARY="sbxgo"

# requested version: positional arg wins, then SBXGO_VERSION env var, else latest.
# Accepts "v0.3.0" or "0.3.0" and normalises to "v0.3.0".
REQUESTED_VERSION="${1:-${SBXGO_VERSION:-}}"
if [[ -n "$REQUESTED_VERSION" && "$REQUESTED_VERSION" != v* ]]; then
  REQUESTED_VERSION="v$REQUESTED_VERSION"
fi

# detect OS/arch

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)        ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
esac

case "$OS/$ARCH" in
  linux/amd64|darwin/arm64) ;;
  *)
    case "$OS" in
      mingw*|msys*|cygwin*|*nt-*)
        cat >&2 <<EOF
ERROR: Detected Windows ($OS). Use install.ps1 instead:
  powershell -c "irm https://raw.githubusercontent.com/${REPO}/main/install.ps1 | iex"
EOF
        exit 1
        ;;
    esac

    cat >&2 <<EOF
ERROR: Unsupported platform: $OS/$ARCH
EOF
    exit 1
    ;;
esac

# resolve install dir

if echo "$PATH" | grep -q "$HOME/bin"; then
  INSTALL_DIR="$HOME/bin"
elif echo "$PATH" | grep -q "$HOME/.local/bin"; then
  INSTALL_DIR="$HOME/.local/bin"
elif [[ -w "/usr/local/bin" ]]; then
  INSTALL_DIR="/usr/local/bin"
else
  INSTALL_DIR="$HOME/.local/bin"
fi

mkdir -p "$INSTALL_DIR"

# check dependencies

command -v curl >/dev/null 2>&1 || { echo "ERROR: curl is required." >&2; exit 1; }

# resolve version

if [[ -n "$REQUESTED_VERSION" ]]; then
  VERSION="$REQUESTED_VERSION"
  echo "Using requested version: $VERSION"
else
  LATEST_URL="https://github.com/${REPO}/releases/latest"
  final_url="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$LATEST_URL")"
  VERSION="${final_url##*/}"

  if [[ -z "$VERSION" || "$VERSION" == "latest" ]]; then
    echo "ERROR: Could not determine latest release version (resolved URL: ${final_url:-<empty>})." >&2
    exit 1
  fi
fi

echo "Installing $BINARY $VERSION ($OS/$ARCH)"

# install binary

asset="${BINARY}_${OS}_${ARCH}"
url="https://github.com/${REPO}/releases/download/${VERSION}/${asset}"
install_path="$INSTALL_DIR/$BINARY"

echo "Downloading $BINARY..."
if ! curl -fsSL "$url" -o "$install_path"; then
  echo "ERROR: Failed to download $BINARY from $url" >&2
  if [[ -n "$REQUESTED_VERSION" ]]; then
    echo "       Check that $REQUESTED_VERSION exists at https://github.com/${REPO}/releases" >&2
  fi
  exit 1
fi

# verify checksum

CHECKSUMS_URL="https://github.com/${REPO}/releases/download/${VERSION}/checksums.txt"
if checksums="$(curl -fsSL "$CHECKSUMS_URL")"; then
  expected="$(echo "$checksums" | awk -v f="$asset" '$2 == f { print $1 }' | head -n1)"

  if [[ -z "$expected" ]]; then
    echo "WARNING: $asset not listed in checksums.txt; skipping verification" >&2
  else
    if command -v sha256sum >/dev/null 2>&1; then
      actual="$(sha256sum "$install_path" | awk '{print $1}')"
    elif command -v shasum >/dev/null 2>&1; then
      actual="$(shasum -a 256 "$install_path" | awk '{print $1}')"
    else
      echo "ERROR: no sha256 utility found (need sha256sum or shasum)" >&2
      rm -f "$install_path"
      exit 1
    fi

    if [[ "$actual" != "$expected" ]]; then
      echo "ERROR: checksum mismatch for $asset" >&2
      echo "  expected: $expected" >&2
      echo "  actual:   $actual" >&2
      rm -f "$install_path"
      exit 1
    fi

    echo "Checksum verified ($expected)."
  fi
else
  echo "WARNING: could not fetch $CHECKSUMS_URL; skipping checksum verification" >&2
fi

chmod +x "$install_path"
echo "Installed $BINARY to $install_path"

# verify

if ! command -v "$BINARY" >/dev/null 2>&1; then
  echo ""
  echo "NOTE: Add $INSTALL_DIR to your PATH:"
  echo "  export PATH=\"$INSTALL_DIR:\$PATH\""
  echo ""
fi

echo ""
echo "Next steps:"
echo "  1. cd into your project repository"
echo "  2. Run: sbxgo setup       # scaffolds .sbxgo/config.toml on first run"
echo "  3. Edit .sbxgo/config.toml"
echo "  4. Everyday use: sbxgo run"
echo ""
echo "Docs: https://github.com/${REPO}"
