#!/usr/bin/env sh
# Install docket onto your PATH.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/dokku/docket/main/install.sh | sh
#   VERSION=0.1.0 curl -fsSL https://raw.githubusercontent.com/dokku/docket/main/install.sh | sh
#
# Environment variables:
#   VERSION  Release tag to install (defaults to the latest release).
#   BIN_DIR  Destination directory (defaults to /usr/local/bin on Linux/macOS,
#            $HOME/bin on Windows shells).
#
# Windows is supported through a POSIX shell: Git Bash, MSYS2, or Cygwin. WSL reports itself as
# Linux, so it installs the native Linux binary to /usr/local/bin rather than docket.exe.

set -eu

REPO="dokku/docket"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
linux | darwin) ;;
mingw* | msys* | cygwin*) os="windows" ;;
*)
  echo "error: unsupported OS: $os" >&2
  exit 1
  ;;
esac

arch="$(uname -m)"
case "$arch" in
x86_64 | amd64) arch="amd64" ;;
aarch64 | arm64) arch="arm64" ;;
*)
  echo "error: unsupported architecture: $arch" >&2
  exit 1
  ;;
esac

if [ -z "${VERSION:-}" ]; then
  VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" |
    sed -n 's/.*"tag_name": "\(.*\)".*/\1/p' | head -n1)"
fi

if [ -z "$VERSION" ]; then
  echo "error: could not determine latest version; set VERSION explicitly" >&2
  exit 1
fi

# Release assets are raw binaries named docket-<os>-<arch>, with a .exe suffix
# on Windows. The installed file uses the same suffix.
ext=""
if [ "$os" = "windows" ]; then
  ext=".exe"
fi

asset="docket-${os}-${arch}${ext}"
url="https://github.com/${REPO}/releases/download/${VERSION}/${asset}"

if [ -z "${BIN_DIR:-}" ]; then
  if [ "$os" = "windows" ]; then
    BIN_DIR="${HOME}/bin"
  else
    BIN_DIR="/usr/local/bin"
  fi
fi

dest="${BIN_DIR}/docket${ext}"

# Decide whether writing to BIN_DIR needs elevation. Check the directory if it
# exists, otherwise the parent it would be created under.
need_sudo=0
if [ -d "$BIN_DIR" ]; then
  [ -w "$BIN_DIR" ] || need_sudo=1
else
  [ -w "$(dirname "$BIN_DIR")" ] || need_sudo=1
fi

maybe_sudo() {
  if [ "$need_sudo" = "1" ]; then
    sudo "$@"
  else
    "$@"
  fi
}

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT INT TERM

echo "downloading ${url}"
curl -fsSL "$url" -o "${tmpdir}/docket${ext}"

if [ "$need_sudo" = "1" ]; then
  echo "elevating with sudo to write to ${BIN_DIR}"
fi
maybe_sudo mkdir -p "$BIN_DIR"
maybe_sudo install -m 0755 "${tmpdir}/docket${ext}" "$dest"

echo "installed docket ${VERSION} to ${dest}"
case ":${PATH}:" in
*":${BIN_DIR}:"*) ;;
*) echo "note: ${BIN_DIR} is not on your PATH; add it to run 'docket' directly" >&2 ;;
esac
echo "try: docket version"
