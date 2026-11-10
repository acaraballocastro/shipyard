#!/usr/bin/env bash

set -e

REPO="acaraballocastro/shipyard"

ARCH=$(uname -m)

if [[ "$ARCH" == "x86_64" ]]; then
    FILE="shipyard-linux-amd64"
elif [[ "$ARCH" == "aarch64" ]]; then
    FILE="shipyard-linux-arm64"
else
    echo "❌ Unsupported architecture"
    exit 1
fi

URL="https://github.com/$REPO/releases/latest/download/$FILE"

echo "📥 Downloading Shipyard..."

curl -L $URL -o shipyard

chmod +x shipyard

echo "📦 Installing Shipyard..."

sudo mv shipyard /usr/local/bin/shipyard

echo "✅ Shipyard installed!"
echo "👉 Next: shipyard setup   (installs Docker if needed, GitHub access, ghcr login)"