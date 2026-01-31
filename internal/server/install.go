package server

import (
	"log/slog"
	"net/http"
	"strings"
)

// InstallScriptHandler serves the install script for curl | sh installation
func InstallScriptHandler(w http.ResponseWriter, r *http.Request) {
	// Log install script downloads for analytics
	ip := ExtractClientIP(r)
	ua := r.Header.Get("User-Agent")
	slog.Info("install script download", "ip", ip, "ua", ua)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(installScript))
}

// ExtractClientIP gets the real client IP, checking X-Forwarded-For for proxies
func ExtractClientIP(r *http.Request) string {
	// Check X-Forwarded-For (set by Cloudflare and other proxies)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP in the chain (original client)
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	// Check CF-Connecting-IP (Cloudflare specific)
	if cfIP := r.Header.Get("CF-Connecting-IP"); cfIP != "" {
		return cfIP
	}
	// Fall back to RemoteAddr
	if idx := strings.LastIndex(r.RemoteAddr, ":"); idx != -1 {
		return r.RemoteAddr[:idx]
	}
	return r.RemoteAddr
}

// IsLikelyBot checks if a User-Agent looks like a bot/crawler/scanner
func IsLikelyBot(ua string) bool {
	ua = strings.ToLower(ua)
	if ua == "" {
		return true
	}
	botPatterns := []string{
		"bot", "crawler", "spider", "scan", "python", "wget", "fetch",
		"http", "axios", "node", "go-http", "java", "ruby", "perl",
		"check", "monitor", "probe", "test", "uptime", "health",
	}
	for _, pattern := range botPatterns {
		if strings.Contains(ua, pattern) {
			return true
		}
	}
	return false
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

# Download to temp dir first, verify, then move
TEMP_DIR=$(mktemp -d)
trap "rm -rf $TEMP_DIR" EXIT

# Download all platform variants
for platform in $PLATFORMS; do
    echo "  Downloading cinch-$platform..."
    DOWNLOAD_URL="https://github.com/$REPO/releases/download/$VERSION/cinch-$platform"
    if curl -fsSL "$DOWNLOAD_URL" -o "$TEMP_DIR/cinch-$platform" 2>/dev/null; then
        chmod +x "$TEMP_DIR/cinch-$platform"
    else
        echo "    Warning: Could not download cinch-$platform (may not exist)"
    fi
done

# Verify local platform binary works before installing
if [ ! -f "$TEMP_DIR/cinch-$LOCAL_PLATFORM" ]; then
    echo "Error: Could not download binary for $LOCAL_PLATFORM"
    exit 1
fi

echo "  Verifying cinch-$LOCAL_PLATFORM..."
if ! "$TEMP_DIR/cinch-$LOCAL_PLATFORM" --version >/dev/null 2>&1; then
    echo "Error: Downloaded binary failed to run - aborting install"
    exit 1
fi

# Move verified binaries into place
for platform in $PLATFORMS; do
    if [ -f "$TEMP_DIR/cinch-$platform" ]; then
        mv -f "$TEMP_DIR/cinch-$platform" "$INSTALL_DIR/cinch-$platform"
    fi
done

# Create symlink for local platform
rm -f "$INSTALL_DIR/cinch"
ln -s "cinch-$LOCAL_PLATFORM" "$INSTALL_DIR/cinch"
echo "Installed cinch $VERSION to $INSTALL_DIR/cinch"

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
