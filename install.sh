#!/usr/bin/env bash
#
# install.sh — install the latest firefly-bridge release
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/rajkumaar23/firefly-bridge/main/install.sh | bash
#
set -euo pipefail

REPO="rajkumaar23/firefly-bridge"
BIN_NAME="firefly-bridge"
RELEASE_URL="https://github.com/${REPO}/releases/latest/download"

# ---------------------------------------------------------------------------
# helpers
# ---------------------------------------------------------------------------

info()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn()  { printf '\033[1;33m warning:\033[0m %s\n' "$*" >&2; }
error() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

cleanup() {
  [ -n "${TMP_DIR:-}" ] && rm -rf "$TMP_DIR"
}
trap cleanup EXIT

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || error "required command '$1' not found"
}

# ---------------------------------------------------------------------------
# preflight
# ---------------------------------------------------------------------------

need_cmd curl
need_cmd awk
need_cmd grep
need_cmd install
need_cmd uname

# ---------------------------------------------------------------------------
# detect platform
# ---------------------------------------------------------------------------

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH_RAW="$(uname -m)"

case "$ARCH_RAW" in
  x86_64|amd64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) error "unsupported architecture: $ARCH_RAW" ;;
esac

case "$OS" in
  linux|darwin) ;;
  *) error "unsupported OS: $OS" ;;
esac

ASSET="${BIN_NAME}-${OS}-${ARCH}"

info "Detected platform: ${OS}/${ARCH}"

# ---------------------------------------------------------------------------
# download binary + checksums
# ---------------------------------------------------------------------------

TMP_DIR="$(mktemp -d)"
ASSET_PATH="${TMP_DIR}/${ASSET}"
CHECKSUMS_PATH="${TMP_DIR}/checksums.txt"

info "Downloading ${ASSET}..."
curl -fsSL -o "$ASSET_PATH" "${RELEASE_URL}/${ASSET}" \
  || error "failed to download ${RELEASE_URL}/${ASSET}"

curl -fsSL -o "$CHECKSUMS_PATH" "${RELEASE_URL}/checksums.txt" \
  || error "failed to download checksums.txt"

# ---------------------------------------------------------------------------
# verify checksum
# ---------------------------------------------------------------------------

info "Verifying checksum..."

EXPECTED_LINE="$(grep -E "^[a-f0-9]+  ?${ASSET}\$" "$CHECKSUMS_PATH" || true)"
[ -n "$EXPECTED_LINE" ] || error "no checksum entry found for ${ASSET}"

VERIFY_INPUT="$(awk -v d="$TMP_DIR" '{print $1"  "d"/"$2}' <<< "$EXPECTED_LINE")"

if command -v shasum >/dev/null 2>&1; then
  shasum -a 256 -c - --status <<< "$VERIFY_INPUT" \
    || error "checksum verification failed"
elif command -v sha256sum >/dev/null 2>&1; then
  sha256sum -c - --status <<< "$VERIFY_INPUT" \
    || error "checksum verification failed"
else
  error "neither shasum nor sha256sum is available"
fi

info "Checksum OK"

# ---------------------------------------------------------------------------
# choose install directory
# ---------------------------------------------------------------------------

if [ "$(id -u)" -eq 0 ] && [ -w /usr/local/bin ]; then
  INSTALL_DIR="/usr/local/bin"
elif [ -w /usr/local/bin ] 2>/dev/null; then
  INSTALL_DIR="/usr/local/bin"
else
  INSTALL_DIR="${HOME}/.local/bin"
  mkdir -p "$INSTALL_DIR"
fi

# ---------------------------------------------------------------------------
# install
# ---------------------------------------------------------------------------

info "Installing to ${INSTALL_DIR}/${BIN_NAME}..."
install -m 755 "$ASSET_PATH" "${INSTALL_DIR}/${BIN_NAME}"

info "Installed $("${INSTALL_DIR}/${BIN_NAME}" --version 2>/dev/null || echo "${BIN_NAME}")"

# ---------------------------------------------------------------------------
# PATH check
# ---------------------------------------------------------------------------

case ":${PATH}:" in
  *":${INSTALL_DIR}:"*)
    info "${INSTALL_DIR} is already in your PATH"
    ;;
  *)
    warn "${INSTALL_DIR} is not in your PATH."
    echo
    echo "  Add this to your shell profile (~/.bashrc, ~/.zshrc, etc.):"
    echo
    echo "    export PATH=\"${INSTALL_DIR}:\$PATH\""
    echo
    ;;
esac

info "Done. Run '${BIN_NAME} --help' to get started."