#!/usr/bin/env bash
# Installs the latest shipyard release (or SHIPYARD_VERSION=vX.Y.Z) into
# /usr/local/bin. Safe to re-run: the current binary is only replaced once
# the download is verified and runs.
set -euo pipefail

REPO="acaraballocastro/shipyard"
DEST="/usr/local/bin/shipyard"

case "$(uname -s)" in
  Linux)  OS=linux ;;
  Darwin) OS=darwin ;;
  *) echo "❌ Unsupported OS: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  x86_64|amd64)  ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  armv7l|armv7)  ARCH=armv7 ;;
  *) echo "❌ Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

FILE="shipyard-$OS-$ARCH"
if [[ -n "${SHIPYARD_VERSION:-}" ]]; then
  BASE="https://github.com/$REPO/releases/download/$SHIPYARD_VERSION"
else
  BASE="https://github.com/$REPO/releases/latest/download"
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "📥 Downloading $FILE ${SHIPYARD_VERSION:-(latest)}..."
curl -fsSL "$BASE/$FILE" -o "$TMP/shipyard"

# Verify the checksum when the release publishes one.
if curl -fsSL "$BASE/$FILE.sha256" -o "$TMP/sum" 2>/dev/null; then
  want="$(cut -d' ' -f1 < "$TMP/sum")"
  if command -v sha256sum >/dev/null; then
    got="$(sha256sum "$TMP/shipyard" | cut -d' ' -f1)"
  else
    got="$(shasum -a 256 "$TMP/shipyard" | cut -d' ' -f1)"
  fi
  if [[ "$want" != "$got" ]]; then
    echo "❌ Checksum mismatch for $FILE" >&2
    exit 1
  fi
  echo "🔒 Checksum verified"
fi

chmod +x "$TMP/shipyard"
if ! VERSION_OUT="$("$TMP/shipyard" --version 2>&1)"; then
  echo "❌ The downloaded binary does not run on this machine:" >&2
  echo "$VERSION_OUT" >&2
  exit 1
fi

echo "📦 Installing to $DEST..."
SUDO=""
if [[ ! -w "$(dirname "$DEST")" ]]; then
  SUDO="sudo"
fi
$SUDO mkdir -p "$(dirname "$DEST")"
$SUDO install -m 0755 "$TMP/shipyard" "$DEST"

echo "✅ $VERSION_OUT installed"
echo "👉 Next: shipyard setup   (installs Docker if needed, GitHub access, ghcr login)"
