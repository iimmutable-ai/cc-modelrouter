#!/bin/bash
#
# install.sh — Install ccrouter binary from GitHub Releases.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/iimmutable-ai/cc-modelrouter/master/scripts/install.sh | bash
#   bash install.sh --version vX.Y.Z -d /custom/path
#
# On Alibaba Cloud / constrained networks where direct github.com fetches are
# truncated or reset, prefix all GitHub URLs with a mirror:
#   GITHUB_MIRROR=https://ghproxy.com bash install.sh
#   (outer curl must also use the mirror — see docs/troubleshooting.md)
#
# The first `ccrouter config` run auto-fetches provider presets from GitHub.

set -euo pipefail

# GITHUB_MIRROR, when set, prefixes https://github.com and https://api.github.com
# URLs so the install works on networks where direct GitHub access is blocked
# or truncated (e.g. Alibaba Cloud). Example: GITHUB_MIRROR=https://ghproxy.com
github_url() {
    if [[ -n "${GITHUB_MIRROR:-}" ]]; then
        printf '%s/%s' "${GITHUB_MIRROR%/}" "$1"
    else
        printf '%s' "$1"
    fi
}

# maybe_export_path ensures BINDIR is on PATH for future shells by appending a
# sentinel-gated export line to the appropriate rc file. No-op if BINDIR is
# already on PATH. When running under sudo, targets the invoking user's rc,
# not root's.
maybe_export_path() {
    local bindir="$1"

    case ":${PATH:-}:" in
        *":${bindir}:"*) return 0 ;;
    esac

    local target_home="${HOME}"
    if [[ -n "${SUDO_USER:-}" && "${SUDO_USER}" != "${USER:-}" ]]; then
        local got
        if got=$(getent passwd "${SUDO_USER}" 2>/dev/null | cut -d: -f6); then
            target_home="${got}"
        fi
    fi

    local shell="${SHELL:-}"
    local rcfile=""
    if [[ "$shell" == */bash ]]; then
        rcfile="${target_home}/.bashrc"
    elif [[ "$shell" == */zsh ]]; then
        local zd="${ZDOTDIR:-}"
        rcfile="${zd:-${target_home}}/.zshrc"
    else
        rcfile="${target_home}/.profile"
    fi

    if [[ ! -f "$rcfile" ]]; then
        touch "$rcfile" 2>/dev/null || return 0
        chmod 600 "$rcfile" 2>/dev/null || true
    fi
    [[ -r "$rcfile" && -w "$rcfile" ]] || return 0

    grep -qF '# ccrouter-path' "$rcfile" 2>/dev/null && return 0

    local needs_leading_newline=0
    if [[ -s "$rcfile" ]]; then
        local last_byte
        last_byte=$(tail -c1 "$rcfile" 2>/dev/null | od -An -c | tr -d ' ')
        if [[ -n "$last_byte" && "$last_byte" != "\\n" ]]; then
            needs_leading_newline=1
        fi
    fi

    {
        (( needs_leading_newline )) && printf '\n'
        printf '\n# ccrouter-path (added by installer)\n'
        printf 'export PATH="%s:$PATH"\n' "$bindir"
    } >> "$rcfile"

    echo "✓ Added ${bindir} to PATH in ${rcfile}"
    echo "  Open a new shell or run: source ${rcfile}"
}

REPO_OWNER="iimmutable-ai"
REPO_NAME="cc-modelrouter"
BINARY_NAME="ccrouter"
DEFAULT_VERSION=""  # empty = latest

# Parse arguments
INSTALL_DIR=""
VERSION="$DEFAULT_VERSION"

while [[ $# -gt 0 ]]; do
    case "$1" in
        --version)
            VERSION="$2"
            shift 2
            ;;
        -d)
            INSTALL_DIR="$2"
            shift 2
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Detect OS and arch
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
    x86_64|amd64)
        ARCH="amd64"
        ;;
    arm64|aarch64)
        ARCH="arm64"
        ;;
    *)
        echo "Unsupported architecture: $ARCH"
        exit 1
        ;;
esac

case "$OS" in
    darwin|linux)
        ;;
    *)
        echo "Unsupported OS: $OS"
        exit 1
        ;;
esac

# Determine install directory
if [[ -n "$INSTALL_DIR" ]]; then
    BINDIR="$INSTALL_DIR"
elif [[ -n "${GOBIN:-}" ]]; then
    BINDIR="$GOBIN"
elif [[ -w "/usr/local/bin" ]]; then
    BINDIR="/usr/local/bin"
else
    # Try with sudo, fallback to ~/.local/bin
    if sudo test -w "/usr/local/bin" 2>/dev/null; then
        BINDIR="/usr/local/bin"
        USE_SUDO=1
    else
        BINDIR="${HOME}/.local/bin"
        mkdir -p "$BINDIR"
    fi
fi

USE_SUDO="${USE_SUDO:-0}"

# Fetch latest release version if not specified
if [[ -z "$VERSION" ]]; then
    API_URL="$(github_url "https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/releases/latest")"
    VERSION=$(curl -fsSL "$API_URL" | grep -m1 '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
    if [[ -z "$VERSION" ]]; then
        echo "Failed to fetch latest release version"
        exit 1
    fi
fi

echo "Installing ${BINARY_NAME} ${VERSION} for ${OS}/${ARCH} to ${BINDIR}"

# Download archive and checksums
ARCHIVE_NAME="${BINARY_NAME}_${VERSION#v}_${OS}_${ARCH}.tar.gz"
DOWNLOAD_URL="$(github_url "https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/download/${VERSION}/${ARCHIVE_NAME}")"
CHECKSUMS_URL="$(github_url "https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/download/${VERSION}/checksums.txt")"

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

ARCHIVE_PATH="${TMPDIR}/${ARCHIVE_NAME}"
CHECKSUMS_PATH="${TMPDIR}/checksums.txt"

echo "Downloading ${ARCHIVE_NAME}..."
curl -fsSL "$DOWNLOAD_URL" -o "$ARCHIVE_PATH"

echo "Downloading checksums..."
curl -fsSL "$CHECKSUMS_URL" -o "$CHECKSUMS_PATH" || {
    echo "Warning: checksums.txt not available, skipping verification"
    CHECKSUMS_PATH=""
}

# Verify checksum if available
if [[ -n "$CHECKSUMS_PATH" && -s "$CHECKSUMS_PATH" ]]; then
    EXPECTED=$(grep -m1 "${ARCHIVE_NAME}" "$CHECKSUMS_PATH" | awk '{print $1}')
    if [[ -n "$EXPECTED" ]]; then
        ACTUAL=$(sha256sum "$ARCHIVE_PATH" | awk '{print $1}')
        if [[ "$ACTUAL" != "$EXPECTED" ]]; then
            echo "Checksum verification failed!"
            echo "Expected: $EXPECTED"
            echo "Actual:   $ACTUAL"
            exit 1
        fi
        echo "Checksum verified ✓"
    fi
fi

# Extract binary
echo "Extracting..."
tar -xzf "$ARCHIVE_PATH" -C "$TMPDIR"

# Install binary
BINARY_SRC="${TMPDIR}/${BINARY_NAME}"
if [[ ! -f "$BINARY_SRC" ]]; then
    echo "Binary not found in archive"
    exit 1
fi

chmod +x "$BINARY_SRC"

if [[ "$USE_SUDO" -eq 1 ]]; then
    sudo mv "$BINARY_SRC" "${BINDIR}/${BINARY_NAME}"
else
    mv "$BINARY_SRC" "${BINDIR}/${BINARY_NAME}"
fi

maybe_export_path "$BINDIR"

echo ""
echo "✓ ${BINARY_NAME} installed to ${BINDIR}/${BINARY_NAME}"
echo ""
echo "Next step: Run '${BINARY_NAME} config' to set up your providers."
echo "           (Provider presets will be auto-fetched from GitHub on first run)"