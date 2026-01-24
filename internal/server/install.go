package server

import (
	"net/http"
)

// InstallScriptHandler serves the install script for curl | sh installation
func InstallScriptHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write([]byte(installScript))
}

const installScript = `#!/bin/sh
# Cinch installer - downloads release binaries from GitHub
# Usage: curl -fsSL https://cinch.sh/install.sh | sh
#
# Installs all platform variants to ~/.cinch/bin/ for container injection support.
# Creates symlink to local platform as 'cinch'.

set -e

REPO="ehrlich-b/cinch"
INSTALL_DIR="$HOME/.cinch/bin"
PLATFORMS="linux-amd64 linux-arm64 darwin-amd64 darwin-arm64"

# Detect local platform
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
    linux)  OS="linux" ;;
    darwin) OS="darwin" ;;
    *)
        echo "Error: Unsupported operating system: $OS"
        exit 1
        ;;
esac

ARCH=$(uname -m)
case "$ARCH" in
    x86_64|amd64)  ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *)
        echo "Error: Unsupported architecture: $ARCH"
        exit 1
        ;;
esac

LOCAL_PLATFORM="$OS-$ARCH"

# Get latest release tag (or use CINCH_VERSION if set)
if [ -n "$CINCH_VERSION" ]; then
    VERSION="$CINCH_VERSION"
else
    VERSION=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
fi

if [ -z "$VERSION" ]; then
    echo "Error: Could not determine version"
    exit 1
fi

echo "Installing cinch $VERSION..."
mkdir -p "$INSTALL_DIR"

# Download all platform variants
for platform in $PLATFORMS; do
    echo "  Downloading cinch-$platform..."
    DOWNLOAD_URL="https://github.com/$REPO/releases/download/$VERSION/cinch-$platform"
    if curl -fsSL "$DOWNLOAD_URL" -o "$INSTALL_DIR/cinch-$platform" 2>/dev/null; then
        chmod +x "$INSTALL_DIR/cinch-$platform"
    else
        echo "    Warning: Could not download cinch-$platform (may not exist)"
    fi
done

# Create symlink for local platform
if [ -f "$INSTALL_DIR/cinch-$LOCAL_PLATFORM" ]; then
    rm -f "$INSTALL_DIR/cinch"
    ln -s "cinch-$LOCAL_PLATFORM" "$INSTALL_DIR/cinch"
    echo "Installed cinch $VERSION to $INSTALL_DIR/cinch"
else
    echo "Error: Could not download binary for $LOCAL_PLATFORM"
    exit 1
fi

# Write version file
echo "$VERSION" > "$INSTALL_DIR/.version"

# Check if install dir is in PATH
case ":$PATH:" in
    *":$INSTALL_DIR:"*)
        echo ""
        echo "Run 'cinch --help' to get started."
        ;;
    *)
        echo ""
        echo "WARNING: $INSTALL_DIR is not in your PATH!"
        echo ""
        echo "To fix this, run:"
        echo ""
        # Detect shell and give appropriate command
        if [ "$(uname -s)" = "Darwin" ]; then
            echo "  echo 'export PATH=\"\$HOME/.cinch/bin:\$PATH\"' >> ~/.zshrc && source ~/.zshrc"
        elif [ -n "$BASH_VERSION" ] || [ "$(basename "$SHELL")" = "bash" ]; then
            echo "  echo 'export PATH=\"\$HOME/.cinch/bin:\$PATH\"' >> ~/.bashrc && source ~/.bashrc"
        else
            echo "  echo 'export PATH=\"\$HOME/.cinch/bin:\$PATH\"' >> ~/.profile"
        fi
        echo ""
        echo "Then run 'cinch --help' to get started."
        ;;
esac
`
