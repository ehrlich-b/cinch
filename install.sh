#!/bin/sh
# Cinch installer - downloads the latest release from GitHub
# Usage: curl -fsSL https://cinch.sh/install.sh | sh

set -e

REPO="ehrlich-b/cinch"
BINARY_NAME="cinch"

# Detect OS
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
    linux)  OS="linux" ;;
    darwin) OS="darwin" ;;
    *)
        echo "Error: Unsupported operating system: $OS"
        exit 1
        ;;
esac

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
    x86_64|amd64)  ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *)
        echo "Error: Unsupported architecture: $ARCH"
        exit 1
        ;;
esac

# Get latest release tag
VERSION=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
if [ -z "$VERSION" ]; then
    echo "Error: Could not determine latest version"
    exit 1
fi

DOWNLOAD_URL="https://github.com/$REPO/releases/download/$VERSION/cinch-$OS-$ARCH"

# Determine install directory
if [ -w "/usr/local/bin" ]; then
    INSTALL_DIR="/usr/local/bin"
elif [ -d "$HOME/.local/bin" ]; then
    INSTALL_DIR="$HOME/.local/bin"
else
    mkdir -p "$HOME/.local/bin"
    INSTALL_DIR="$HOME/.local/bin"
fi

echo "Downloading cinch $VERSION for $OS/$ARCH..."
curl -fsSL "$DOWNLOAD_URL" -o "$INSTALL_DIR/$BINARY_NAME"
chmod +x "$INSTALL_DIR/$BINARY_NAME"

echo "Installed cinch $VERSION to $INSTALL_DIR/$BINARY_NAME"

# Check if install dir is in PATH
case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *)
        echo ""
        echo "Note: $INSTALL_DIR is not in your PATH."
        echo "Add it with: export PATH=\"\$PATH:$INSTALL_DIR\""
        ;;
esac

echo ""
echo "Run 'cinch --help' to get started."
