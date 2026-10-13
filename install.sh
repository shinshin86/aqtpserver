#!/usr/bin/env bash
set -euo pipefail

OWNER="shinshin86"
REPO="aqtpserver"
BIN_NAME="aqtpserver"
BIN_DIR="${BIN_DIR:-${HOME}/.local/bin}"

err() {
  echo "error: $*" >&2
  exit 1
}

if [ "$#" -gt 1 ]; then
  err "Usage: bash install.sh [vX.Y.Z|latest]"
fi

for cmd in curl tar install shasum awk; do
  command -v "$cmd" >/dev/null 2>&1 || err "$cmd is required."
done

version="${1:-latest}"
if [ "$version" != "latest" ] && ! [[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  err "Invalid version format. Use vX.Y.Z or 'latest'."
fi

os=$(uname -s)
[ "$os" = "Darwin" ] || err "Unsupported OS: $os. This installer requires macOS."
arch=$(uname -m)
case "$arch" in
  arm64|aarch64) arch="arm64" ;;
  x86_64|amd64) arch="amd64" ;;
  *) err "Unsupported architecture: $arch" ;;
esac

if [ "$version" = "latest" ]; then
  version=$(curl --retry 3 -fsSL "https://api.github.com/repos/${OWNER}/${REPO}/releases/latest" |
    sed -nE 's/.*"tag_name"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/p')
fi
[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || err "Failed to determine a release version."

asset="${BIN_NAME}_${version}_darwin_${arch}.tar.gz"
base_url="https://github.com/${OWNER}/${REPO}/releases/download/${version}"
tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT

echo "Downloading $asset..."
curl --retry 3 -fsSL "${base_url}/checksums.txt" -o "${tmp_dir}/checksums.txt"
expected_checksum=$(awk -v asset="$asset" '$2 == asset { print $1 }' "${tmp_dir}/checksums.txt")
[[ "$expected_checksum" =~ ^[0-9a-fA-F]{64}$ ]] || err "Missing or invalid checksum for $asset."
curl --retry 3 -fsSL "${base_url}/${asset}" -o "${tmp_dir}/${asset}"
actual_checksum=$(shasum -a 256 "${tmp_dir}/${asset}" | awk '{print $1}')
[ "$expected_checksum" = "$actual_checksum" ] || err "Checksum mismatch for $asset."

tar -xzf "${tmp_dir}/${asset}" -C "$tmp_dir" "$BIN_NAME"
[ -f "${tmp_dir}/${BIN_NAME}" ] && [ ! -L "${tmp_dir}/${BIN_NAME}" ] || err "Binary missing from release archive."
mkdir -p "$BIN_DIR"
if [ -f "${BIN_DIR}/${BIN_NAME}" ]; then
  cp "${BIN_DIR}/${BIN_NAME}" "${BIN_DIR}/${BIN_NAME}.bak"
  echo "Existing binary backed up to ${BIN_NAME}.bak"
fi
install -m 755 "${tmp_dir}/${BIN_NAME}" "${BIN_DIR}/${BIN_NAME}"
echo "Installed $BIN_NAME to $BIN_DIR"
if [[ ":${PATH}:" != *":${BIN_DIR}:"* ]]; then
  echo "Add this directory to PATH: $BIN_DIR"
fi
echo "Run 'aqtpserver --version' to verify. Install AquesTalkPlayer separately."
