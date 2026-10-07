#!/bin/sh
# Werkbord Team installer for macOS and Linux. This is Team's own installer: it
# installs the Team product, which is a different program from the individual
# Werkbord (scripts/install.sh) and is released and versioned separately.
#
#   curl -fsSL https://raw.githubusercontent.com/micho8cho93/werkbord/main/scripts/install-team.sh | sh
#
# It downloads the Team release for this computer, checks it against the release's
# published checksums, and puts the executable, werkbord-team, in ~/.local/bin. A release
# that carries the network program Team supervises (docs/TEAM_NETWORK.md) also gets it,
# in ~/.local/libexec/werkbord-team, with its licences in ~/.local/share/doc/werkbord-team.
# It starts nothing and installs no service; see docs/TEAM.md for what to do next.
#
# Environment:
#   WERKBORD_TEAM_VERSION      install this release (v1.2.3 or werkbord-team-v1.2.3) instead of the latest
#   WERKBORD_TEAM_INSTALL_DIR  where the executable goes (default ~/.local/bin)
#   WERKBORD_TEAM_BASE_URL     where releases are (default: this project's GitHub releases)
#   WERKBORD_TEAM_FEED_URL     the releases feed used to find the latest Team release (default: <base>.atom)
set -eu

REPO="micho8cho93/werkbord"
BASE=${WERKBORD_TEAM_BASE_URL:-"https://github.com/$REPO/releases"}
BASE=${BASE%/}
FEED=${WERKBORD_TEAM_FEED_URL:-"$BASE.atom"}

say() { printf '%s\n' "$*"; }
fail() { printf 'werkbord-team install: %s\n' "$*" >&2; exit 1; }

os=${WERKBORD_TEAM_OS:-$(uname -s)}
case "$os" in
  Darwin|darwin) os=darwin ;;
  Linux|linux) os=linux ;;
  *) fail "no Team release is built for $os (macOS and Linux only). Build from source instead: make build-team" ;;
esac
arch=${WERKBORD_TEAM_ARCH:-$(uname -m)}
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) fail "no Team release is built for $arch ($os). Build from source instead: make build-team" ;;
esac

if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -q -O "$2" "$1"; }
else
  fail "curl or wget is needed to download Werkbord Team"
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

tmp=$(mktemp -d "${TMPDIR:-/tmp}/werkbord-team-install.XXXXXX")
trap 'rm -rf "$tmp"' EXIT INT TERM

# ---- which release ----
# The repository releases two products, and /releases/latest belongs to the individual
# one, so use Team's stable release links in the feed. Release descriptions can
# contain old tags, comparison URLs and prereleases; they are not release entries.
tag=${WERKBORD_TEAM_VERSION:-}
if [ -z "$tag" ]; then
  say "Looking for the latest Werkbord Team release..."
  fetch "$FEED" "$tmp/feed" || fail "could not read the releases feed at $FEED"
  tag=$(awk -v RS='<' '
    /^link[[:space:]]/ && match($0, /href="[^"]*\/releases\/tag\/werkbord-team-v[0-9]+\.[0-9]+\.[0-9]+"/) {
      tag = substr($0, RSTART, RLENGTH)
      sub(/^.*\//, "", tag); sub(/"$/, "", tag)
      print tag; exit
    }
  ' "$tmp/feed")
  [ -n "$tag" ] || fail "no Werkbord Team release has been published yet"
fi
case "$tag" in
  werkbord-team-v[0-9]*.[0-9]*.[0-9]*) version=v${tag#werkbord-team-v} ;;
  v[0-9]*.[0-9]*.[0-9]*) version=$tag; tag=werkbord-team-$tag ;;
  [0-9]*.[0-9]*.[0-9]*) version=v$tag; tag=werkbord-team-v$tag ;;
  werkbord-v[0-9]*|devboard*) fail "\"$tag\" is an individual Werkbord release. This installer is for Werkbord Team; the individual product has its own (scripts/install.sh)" ;;
  *) fail "\"$tag\" is not a Team release version (expected something like v1.2.3)" ;;
esac

asset="werkbord-team_${version#v}_${os}_${arch}.tar.gz"
say "Installing Werkbord Team $version for $os/$arch"
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

mkdir -p "$tmp/unpack"
tar -xzf "$tmp/$asset" -C "$tmp/unpack" || fail "could not unpack $asset"
bin=$(find "$tmp/unpack" -type f -name werkbord-team | head -1)
[ -n "$bin" ] || fail "$asset does not contain werkbord-team"
chmod +x "$bin"
reported=$("$bin" version 2>/dev/null || true)
[ "$reported" = "$version" ] || fail "the downloaded executable says it is \"$reported\", not $version: not installing it"

dir=${WERKBORD_TEAM_INSTALL_DIR:-"$HOME/.local/bin"}
mkdir -p "$dir" || fail "cannot create $dir (set WERKBORD_TEAM_INSTALL_DIR to somewhere you can write)"
cp "$bin" "$dir/.werkbord-team.new" || fail "cannot write to $dir (set WERKBORD_TEAM_INSTALL_DIR to somewhere you can write)"
chmod 755 "$dir/.werkbord-team.new"
mv -f "$dir/.werkbord-team.new" "$dir/werkbord-team"
say "Installed $dir/werkbord-team ($version)"

# The network program Team supervises, if this release carries it: beside the executable's own libexec directory
# (where Team looks, and checks it against the version it was built to run), with the licences that go with it.
# Nothing is fetched when Team runs, so this is the only place it comes from.
nebula=$(find "$tmp/unpack" -type f -path '*/libexec/werkbord-team/nebula' | head -1)
if [ -n "$nebula" ]; then
  prefix=$(dirname "$dir")
  lib="$prefix/libexec/werkbord-team"
  mkdir -p "$lib" || fail "cannot create $lib"
  cp "$nebula" "$lib/.nebula.new" && chmod 755 "$lib/.nebula.new" && mv -f "$lib/.nebula.new" "$lib/nebula"
  say "Installed the network program (Nebula) at $lib/nebula"
  lic=$(find "$tmp/unpack" -type d -name licenses | head -1)
  if [ -n "$lic" ]; then
    doc="$prefix/share/doc/werkbord-team"
    mkdir -p "$doc" && rm -rf "$doc/licenses" && cp -R "$lic" "$doc/licenses" && say "Licences are in $doc/licenses"
  fi
fi
# The database program Team supervises, the same way, if this release carries it.
rqlited=$(find "$tmp/unpack" -type f -path '*/libexec/werkbord-team/rqlited' | head -1)
if [ -n "$rqlited" ]; then
  prefix=$(dirname "$dir")
  lib="$prefix/libexec/werkbord-team"
  mkdir -p "$lib" || fail "cannot create $lib"
  cp "$rqlited" "$lib/.rqlited.new" && chmod 755 "$lib/.rqlited.new" && mv -f "$lib/.rqlited.new" "$lib/rqlited"
  record=$(dirname "$rqlited")/rqlited.build
  [ ! -f "$record" ] || cp "$record" "$lib/rqlited.build"
  say "Installed the database program (rqlite) at $lib/rqlited"
fi
case ":$PATH:" in
  *":$dir:"*) ;;
  *) say "Note: $dir is not on your PATH. Add it to your shell profile:
  export PATH=\"$dir:\$PATH\"" ;;
esac
say ""
say "Next:"
say "  werkbord-team workspace create --name \"Your team\" --owner \"Your name\""
say "  werkbord-team serve"
