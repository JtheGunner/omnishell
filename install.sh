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

tarball="omnishell_${os}_${arch}.tar.gz"
url="https://github.com/$repo/releases/download/$tag/$tarball"
sums_url="https://github.com/$repo/releases/download/$tag/checksums.txt"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "downloading $url"
curl -fsSL "$url" -o "$tmp/$tarball"
curl -fsSL "$sums_url" -o "$tmp/checksums.txt"

expected="$(sed -n "s/^\\([0-9a-f]\\{64\\}\\)  *$tarball\$/\\1/p" "$tmp/checksums.txt")"
[ -n "$expected" ] || { echo "no checksum for $tarball in checksums.txt" >&2; exit 1; }

verify() {
  # $@ is the checksum tool + args; reads "<hash>  <name>" on stdin.
  ( cd "$tmp" && printf '%s  %s\n' "$expected" "$tarball" | "$@" -c - ) >/dev/null 2>&1
}

if command -v sha256sum >/dev/null 2>&1; then
  verify sha256sum || { echo "checksum verification failed for $tarball" >&2; exit 1; }
  echo "checksum OK ($expected)"
elif command -v shasum >/dev/null 2>&1; then
  verify shasum -a 256 || { echo "checksum verification failed for $tarball" >&2; exit 1; }
  echo "checksum OK ($expected)"
else
  echo "warning: no sha256 tool, skipping verification" >&2
fi

tar -xz -f "$tmp/$tarball" -C "$tmp"
mkdir -p "$bin_dir"
install -m 0755 "$tmp/omnishell" "$bin_dir/omnishell"
echo "installed omnishell $tag to $bin_dir/omnishell"

# Best-effort: install the bundled shell completions and man page into XDG
# locations. Never fail the install over these.
data_dir="${XDG_DATA_HOME:-$HOME/.local/share}"
if [ -f "$tmp/completions/omnishell.bash" ]; then
  mkdir -p "$data_dir/bash-completion/completions" \
    && cp "$tmp/completions/omnishell.bash" "$data_dir/bash-completion/completions/omnishell" \
    && echo "installed bash completion to $data_dir/bash-completion/completions/omnishell" || true
fi
if [ -f "$tmp/completions/_omnishell" ]; then
  mkdir -p "$data_dir/zsh/site-functions" \
    && cp "$tmp/completions/_omnishell" "$data_dir/zsh/site-functions/_omnishell" \
    && echo "installed zsh completion to $data_dir/zsh/site-functions/_omnishell (ensure that dir is in \$fpath)" || true
fi
if [ -d "$tmp/man" ]; then
  mkdir -p "$data_dir/man/man1" \
    && cp "$tmp"/man/*.1 "$data_dir/man/man1/" 2>/dev/null \
    && echo "installed man pages to $data_dir/man/man1/" || true
fi

echo "make sure $bin_dir is on your PATH, then run: omnishell init"
