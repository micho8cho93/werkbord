#!/bin/sh
# Dev Board installer for macOS and Linux.
#
#   curl -fsSL https://raw.githubusercontent.com/micho8cho93/dev-board/main/scripts/install.sh | sh
#
# It downloads the release for this computer, checks it against the release's
# published checksums, puts it in ~/.local/bin, and runs `devboard setup`: the data
# directory, the database, a background service that starts when you log in, this
# computer as your first runner, phone access, and then it opens the app in your
# browser. It needs no account, no Docker and no root; it installs nothing but one
# executable (and the service definition setup writes in your home directory).
#
# Pass options to setup after `sh -s --`, e.g.:
#   curl -fsSL .../install.sh | sh -s -- --no-network
#
# Environment:
#   DEVBOARD_VERSION      install this release (e.g. v1.2.3, or its tag werkbord-v1.2.3) instead of the latest
#   DEVBOARD_INSTALL_DIR  where the executable goes (default ~/.local/bin)
#   DEVBOARD_BASE_URL     where releases are (default: this project's GitHub releases)
#   DEVBOARD_NO_SETUP=1   only install the executable
#   DEVBOARD_NO_SERVICE, DEVBOARD_NO_OPEN, DEVBOARD_NO_NETWORK, DEVBOARD_NO_START
#                         passed on to setup (see `devboard setup -h`)
set -eu

REPO="micho8cho93/dev-board"
BASE=${DEVBOARD_BASE_URL:-"https://github.com/$REPO/releases"}
BASE=${BASE%/}

say() { printf '%s\n' "$*"; }
fail() { printf 'devboard install: %s\n' "$*" >&2; exit 1; }

# ---- this computer ----

os=${DEVBOARD_OS:-$(uname -s)}
case "$os" in
  Darwin|darwin) os=darwin ;;
  Linux|linux) os=linux ;;
  MINGW*|MSYS*|CYGWIN*|Windows*) fail "this is the macOS and Linux installer. On Windows run, in PowerShell:
  irm https://raw.githubusercontent.com/$REPO/main/scripts/install.ps1 | iex
(or run this installer inside WSL, which is where coding agents run best)." ;;
  *) fail "no release is built for $os. Build from source instead: https://github.com/$REPO" ;;
esac

arch=${DEVBOARD_ARCH:-$(uname -m)}
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) fail "no release is built for $arch ($os). Build from source instead: https://github.com/$REPO" ;;
esac

# ---- what is available to do the work ----

if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL "$1" -o "$2"; }
  resolve_latest() { curl -fsSLI -o /dev/null -w '%{url_effective}' "$BASE/latest"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -q -O "$2" "$1"; }
  resolve_latest() { wget -q --max-redirect=5 --spider -S "$BASE/latest" 2>&1 | sed -n 's/.*[Ll]ocation: *//p' | tail -1; }
else
  fail "curl or wget is needed to download Dev Board"
fi

if command -v sha256sum >/dev/null 2>&1; then
  sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
  sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
elif command -v openssl >/dev/null 2>&1; then
  sha256() { openssl dgst -sha256 "$1" | sed 's/^.*= *//'; }
else
  fail "sha256sum, shasum or openssl is needed to check the download"
fi
command -v tar >/dev/null 2>&1 || fail "tar is needed to unpack the download"

# ---- which release ----

# A release is named by its product's tag (werkbord-v1.2.3; the earliest releases
# were a bare v1.2.3). The executable reports the bare version, v1.2.3.
tag=${DEVBOARD_VERSION:-}
explicit=${tag:+1}
if [ -z "$tag" ]; then
  say "Looking for the latest release..."
  url=$(resolve_latest) || fail "could not look up the latest release at $BASE/latest"
  tag=${url##*/}
fi
case "$tag" in
  werkbord-v[0-9]*.[0-9]*.[0-9]*) version=v${tag#werkbord-v} ;;
  v[0-9]*.[0-9]*.[0-9]*|[0-9]*.[0-9]*.[0-9]*)
    version=v${tag#v}
    if [ -n "$explicit" ]; then
      # Asked for by version: releases before 0.8.0 have the bare tag, later ones the product tag.
      num=${version#v}; major=${num%%.*}; rest=${num#*.}; minor=${rest%%.*}
      if [ "$major" -eq 0 ] && [ "$minor" -lt 8 ]; then tag=$version; else tag=werkbord-$version; fi
    fi ;;
  werkbord-team-*) fail "\"$tag\" is a Werkbord Team release. This installer is for the individual product; Team has its own (scripts/install-team.sh)" ;;
  *) fail "\"$tag\" is not a release version (expected something like v1.2.3)" ;;
esac

asset="devboard_${version#v}_${os}_${arch}.tar.gz"
tmp=$(mktemp -d "${TMPDIR:-/tmp}/devboard-install.XXXXXX")
trap 'rm -rf "$tmp"' EXIT INT TERM

say "Installing Dev Board $version for $os/$arch"
fetch "$BASE/download/$tag/checksums.txt" "$tmp/checksums.txt" || fail "could not download the checksums for $tag"
want=$(awk -v f="$asset" '{ n=$2; sub(/^\*/, "", n); if (n == f) print $1 }' "$tmp/checksums.txt")
[ -n "$want" ] || fail "release $tag has no build for $os/$arch"
fetch "$BASE/download/$tag/$asset" "$tmp/$asset" || fail "could not download $asset"
got=$(sha256 "$tmp/$asset")
if [ "$got" != "$want" ]; then
  fail "the download does not match its published checksum, so it was not installed
  expected $want
  got      $got"
fi

# ---- install ----

mkdir -p "$tmp/unpack"
tar -xzf "$tmp/$asset" -C "$tmp/unpack" devboard 2>/dev/null || tar -xzf "$tmp/$asset" -C "$tmp/unpack" || fail "could not unpack $asset"
bin=$(find "$tmp/unpack" -type f -name devboard | head -1)
[ -n "$bin" ] || fail "$asset does not contain devboard"
chmod +x "$bin"
reported=$("$bin" version 2>/dev/null || true)
[ "$reported" = "$version" ] || fail "the downloaded executable says it is \"$reported\", not $version: not installing it"

dir=${DEVBOARD_INSTALL_DIR:-"$HOME/.local/bin"}
mkdir -p "$dir" || fail "cannot create $dir (set DEVBOARD_INSTALL_DIR to somewhere you can write)"
# Written beside its destination and renamed into place, so that replacing a
# running devboard (an upgrade) is atomic and never leaves half a file.
cp "$bin" "$dir/.devboard.new" || fail "cannot write to $dir (set DEVBOARD_INSTALL_DIR to somewhere you can write)"
chmod 755 "$dir/.devboard.new"
mv -f "$dir/.devboard.new" "$dir/devboard"
say "Installed $dir/devboard"

case ":$PATH:" in
  *":$dir:"*) ;;
  *) say "Note: $dir is not on your PATH. Add it to your shell profile to run \`devboard\` from anywhere:
  export PATH=\"$dir:\$PATH\"" ;;
esac

if [ -n "${DEVBOARD_NO_SETUP:-}" ]; then
  say "Done. Run \`$dir/devboard setup\` to finish."
  exit 0
fi

# ---- setup ----

say ""
rm -rf "$tmp" # exec replaces this shell, so the trap would never run
trap - EXIT INT TERM
exec "$dir/devboard" setup "$@"
