#!/bin/sh
# Install the latest omnishell release into ~/.local/bin (override with OMNISHELL_BIN_DIR).
set -eu

repo="JtheGunner/omnishell"
bin_dir="${OMNISHELL_BIN_DIR:-$HOME/.local/bin}"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
  linux|darwin) : ;;
  *) echo "unsupported OS: $os" >&2; exit 1 ;;
esac

arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "unsupported arch: $arch" >&2; exit 1 ;;
esac

tag="$(curl -fsSL "https://api.github.com/repos/$repo/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p')"
[ -n "$tag" ] || { echo "could not determine latest release" >&2; exit 1; }

url="https://github.com/$repo/releases/download/$tag/omnishell_${os}_${arch}.tar.gz"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "downloading $url"
curl -fsSL "$url" | tar -xz -C "$tmp"
mkdir -p "$bin_dir"
install -m 0755 "$tmp/omnishell" "$bin_dir/omnishell"
echo "installed omnishell $tag to $bin_dir/omnishell"
echo "make sure $bin_dir is on your PATH, then run: omnishell init"
