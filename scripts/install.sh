#!/usr/bin/env bash

set -euo pipefail

REPO="janekbaraniewski/openusage"
BINARY_NAME="openusage"
INSTALL_DIR="${OPENUSAGE_INSTALL_DIR:-}"
VERSION="${OPENUSAGE_VERSION:-}"

log() {
  printf '==> %s\n' "$*"
}

die() {
  printf 'Error: %s\n' "$*" >&2
  exit 1
}

has_cmd() {
  command -v "$1" >/dev/null 2>&1
}

need_cmd() {
  has_cmd "$1" || die "required command not found: $1"
}

usage() {
  cat <<'EOF'
Install OpenUsage from GitHub Releases.

Usage:
  install.sh [--version vX.Y.Z] [--install-dir PATH]

Options:
  --version      Install a specific version (default: latest release)
  --install-dir  Installation directory (default: /usr/local/bin if writable, otherwise ~/.local/bin; on Windows: ~/bin)
  -h, --help     Show this help

Environment variables:
  OPENUSAGE_VERSION      Same as --version
  OPENUSAGE_INSTALL_DIR  Same as --install-dir
  OPENUSAGE_GITHUB_TOKEN Optional GitHub token (helps avoid API rate limits)
EOF
}

api_get() {
  local url="$1"
  if has_cmd curl; then
    if [ -n "${OPENUSAGE_GITHUB_TOKEN:-}" ]; then
      curl -fsSL \
        -H "Accept: application/vnd.github+json" \
        -H "Authorization: Bearer ${OPENUSAGE_GITHUB_TOKEN}" \
        "$url"
    else
      curl -fsSL -H "Accept: application/vnd.github+json" "$url"
    fi
    return
  fi

  if has_cmd wget; then
    if [ -n "${OPENUSAGE_GITHUB_TOKEN:-}" ]; then
      wget -qO- \
        --header="Accept: application/vnd.github+json" \
        --header="Authorization: Bearer ${OPENUSAGE_GITHUB_TOKEN}" \
        "$url"
    else
      wget -qO- --header="Accept: application/vnd.github+json" "$url"
    fi
    return
  fi

  die "either curl or wget is required"
}

download_to() {
  local url="$1"
  local out="$2"
  if has_cmd curl; then
    curl -fL --progress-bar "$url" -o "$out"
    return
  fi

  if has_cmd wget; then
    wget -q "$url" -O "$out"
    return
  fi

  die "either curl or wget is required"
}

verify_checksum_if_available() {
  local archive="$1"
  local asset="$2"
  local version_tag="$3"
  local checksum_file="$4"
  local expected=""
  local actual=""

  download_to \
    "https://github.com/${REPO}/releases/download/${version_tag}/checksums.txt" \
    "$checksum_file" || return 0

  expected="$(grep "[[:space:]]${asset}\$" "$checksum_file" | awk '{print $1}' || true)"
  if [ -z "$expected" ]; then
    log "No checksum entry found for ${asset}; skipping checksum verification."
    return 0
  fi

  if has_cmd sha256sum; then
    actual="$(sha256sum "$archive" | awk '{print $1}')"
  elif has_cmd shasum; then
    actual="$(shasum -a 256 "$archive" | awk '{print $1}')"
  else
    log "No sha256 tool found; skipping checksum verification."
    return 0
  fi

  if [ "$actual" != "$expected" ]; then
    die "checksum mismatch for ${asset}"
  fi
  log "Checksum verification passed."
}

normalize_version_tag() {
  local v="$1"
  if [ -z "$v" ]; then
    printf '%s' ""
    return 0
  fi
  case "$v" in
    v*) printf '%s' "$v" ;;
    *) printf 'v%s' "$v" ;;
  esac
}

detect_platform() {
  local os_raw arch_raw os arch

  os_raw="$(uname -s)"
  arch_raw="$(uname -m)"

  case "$os_raw" in
    Linux) os="linux" ;;
    Darwin) os="darwin" ;;
    MINGW*|MSYS*|CYGWIN*) os="windows" ;;
    *)
      die "unsupported OS for this script: ${os_raw}"
      ;;
  esac

  case "$arch_raw" in
    x86_64|amd64) arch="amd64" ;;
    arm64|aarch64) arch="arm64" ;;
    *) die "unsupported architecture: ${arch_raw}" ;;
  esac

  printf '%s %s' "$os" "$arch"
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version)
      [ "$#" -ge 2 ] || die "--version requires a value"
      VERSION="$2"
      shift 2
      ;;
    --install-dir)
      [ "$#" -ge 2 ] || die "--install-dir requires a value"
      INSTALL_DIR="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

need_cmd uname

read -r OS ARCH <<EOF
$(detect_platform)
EOF

if [ "$OS" = "windows" ]; then
  [ "$ARCH" = "amd64" ] || die "Windows arm64 binaries are not published yet"
  BINARY_NAME="openusage.exe"
  ARCHIVE_EXT="zip"
else
  BINARY_NAME="openusage"
  ARCHIVE_EXT="tar.gz"
fi

if [ -z "$INSTALL_DIR" ]; then
  if [ "$OS" = "windows" ]; then
    INSTALL_DIR="${HOME}/bin"
  elif [ -d "/usr/local/bin" ] && [ -w "/usr/local/bin" ]; then
    INSTALL_DIR="/usr/local/bin"
  else
    INSTALL_DIR="${HOME}/.local/bin"
  fi
fi

VERSION_TAG="$(normalize_version_tag "$VERSION")"
if [ -z "$VERSION_TAG" ]; then
  log "Resolving latest release version..."
  RELEASE_JSON="$(api_get "https://api.github.com/repos/${REPO}/releases/latest")"
  VERSION_TAG="$(printf '%s' "$RELEASE_JSON" | tr -d '\n' | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
  [ -n "$VERSION_TAG" ] || die "failed to resolve latest release tag from GitHub API"
fi

VERSION_NO_V="${VERSION_TAG#v}"
ASSET="openusage_${VERSION_NO_V}_${OS}_${ARCH}.${ARCHIVE_EXT}"
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION_TAG}/${ASSET}"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

ARCHIVE_PATH="${TMP_DIR}/${ASSET}"
CHECKSUM_PATH="${TMP_DIR}/checksums.txt"

log "Downloading ${ASSET}..."
download_to "$DOWNLOAD_URL" "$ARCHIVE_PATH" || die "failed to download asset: ${DOWNLOAD_URL}"

verify_checksum_if_available "$ARCHIVE_PATH" "$ASSET" "$VERSION_TAG" "$CHECKSUM_PATH"

log "Extracting archive..."
case "$ARCHIVE_EXT" in
  tar.gz)
    need_cmd tar
    tar -xzf "$ARCHIVE_PATH" -C "$TMP_DIR"
    ;;
  zip)
    need_cmd unzip
    unzip -q "$ARCHIVE_PATH" -d "$TMP_DIR"
    ;;
  *)
    die "unsupported archive format: ${ARCHIVE_EXT}"
    ;;
esac

BIN_PATH="$(find "$TMP_DIR" -type f -name "$BINARY_NAME" | head -n 1 || true)"
[ -n "$BIN_PATH" ] || die "could not find ${BINARY_NAME} in extracted archive"

mkdir -p "$INSTALL_DIR"
if [ ! -w "$INSTALL_DIR" ]; then
  die "install directory is not writable: ${INSTALL_DIR}. Re-run with a writable path."
fi

if has_cmd install; then
  install -m 0755 "$BIN_PATH" "${INSTALL_DIR}/${BINARY_NAME}"
else
  cp "$BIN_PATH" "${INSTALL_DIR}/${BINARY_NAME}"
  chmod 0755 "${INSTALL_DIR}/${BINARY_NAME}"
fi

log "Installed ${BINARY_NAME} ${VERSION_TAG} to ${INSTALL_DIR}/${BINARY_NAME}"

case ":${PATH}:" in
  *":${INSTALL_DIR}:"*) ;;
  *)
    printf '\n'
    printf 'Add %s to your PATH to run %s directly.\n' "$INSTALL_DIR" "$BINARY_NAME"
    ;;
esac

printf '\n'
printf 'Run: %s\n' "${INSTALL_DIR}/${BINARY_NAME}"
