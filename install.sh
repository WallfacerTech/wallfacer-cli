#!/bin/sh
set -e

REPO="WallfacerTech/wallfacer-cli"
INSTALL_DIR="${HOME}/.local/bin"
BINARY="wallfacer"

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

case "$OS" in
  darwin|linux) ;;
  *) echo "Unsupported OS: $OS" >&2; exit 1 ;;
esac

VERSION=$(curl -s "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | cut -d '"' -f 4)
if [ -z "$VERSION" ]; then
  echo "Failed to fetch latest version" >&2
  exit 1
fi

VERSION_NUM=$(echo "$VERSION" | sed 's/^v//')
URL="https://github.com/${REPO}/releases/download/${VERSION}/wallfacer-cli_${VERSION_NUM}_${OS}_${ARCH}.tar.gz"

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

mkdir -p "$INSTALL_DIR"

echo "Downloading wallfacer ${VERSION}..."
curl -sL "$URL" -o "${TMPDIR}/wallfacer.tar.gz"
tar -xzf "${TMPDIR}/wallfacer.tar.gz" -C "$TMPDIR"

mv "${TMPDIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
chmod +x "${INSTALL_DIR}/${BINARY}"
echo "Installed wallfacer ${VERSION} to ${INSTALL_DIR}/${BINARY}"
