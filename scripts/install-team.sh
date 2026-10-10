#!/bin/sh
# Werkbord Team installer for macOS and Linux, for a Workspace Host or any computer that runs the Team
# service from the command line. Werkbord is one release with one version: this installs the Team executable
# of that same release (the individual one is scripts/install.sh), and a Mac with the Werkbord app needs neither.
#
#   sh /trusted/installer/install-team.sh
# Obtain this script and the release verification PEM through an independently trusted channel.
#
# It downloads the Team release for this computer, checks it against the release's
# vendor-signed checksums, and puts the executable, werkbord-team, in ~/.local/bin. A release
# that carries the network program Team supervises (docs/TEAM_NETWORK.md) also gets it,
# in ~/.local/libexec/werkbord-team, with its licences in ~/.local/share/doc/werkbord-team.
# It starts nothing and installs no service; see docs/TEAM.md for what to do next.
#
# Environment:
#   WERKBORD_TEAM_VERSION      install this release (v1.2.3 or werkbord-v1.2.3; a preview such as v4.0.0-preview.1 only by name) instead of the latest stable one
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
# There is one release series, werkbord-vX.Y.Z, and /releases/latest is the one the individual installer follows, so use the
# stable release links in the feed. Release descriptions can contain old tags, comparison URLs and prereleases; they are not
# release entries.
tag=${WERKBORD_TEAM_VERSION:-}
if [ -z "$tag" ]; then
  say "Looking for the latest Werkbord release..."
  fetch "$FEED" "$tmp/feed" || fail "could not read the releases feed at $FEED"
  tag=$(awk -v RS='<' '
    /^link[[:space:]]/ && match($0, /href="[^"]*\/releases\/tag\/werkbord-v[0-9]+\.[0-9]+\.[0-9]+"/) {
      tag = substr($0, RSTART, RLENGTH)
      sub(/^.*\//, "", tag); sub(/"$/, "", tag)
      print tag; exit
    }
  ' "$tmp/feed")
  [ -n "$tag" ] || fail "no stable Werkbord release has been published yet (name a preview with WERKBORD_TEAM_VERSION=v4.0.0-preview.1)"
fi
case "$tag" in
  werkbord-v[0-9]*.[0-9]*.[0-9]*) version=v${tag#werkbord-v} ;;
  v[0-9]*.[0-9]*.[0-9]*) version=$tag; tag=werkbord-$tag ;;
  [0-9]*.[0-9]*.[0-9]*) version=v$tag; tag=werkbord-v$tag ;;
  werkbord-team-v[0-9]*) fail "\"$tag\" is from the retired Team release series. Werkbord Team is released with Werkbord now: name a werkbord-vX.Y.Z release (4.0.0-preview.1 was the first)" ;;
  devboard*) fail "\"$tag\" is a release from before the rename; name a werkbord-vX.Y.Z release" ;;
  *) fail "\"$tag\" is not a release version (expected something like v4.0.0)" ;;
esac

asset="werkbord-team_${version#v}_${os}_${arch}.tar.gz"
case "$tag" in *[!A-Za-z0-9.+-]*) fail "invalid characters in release identity" ;; esac
say "Installing Werkbord Team $version for $os/$arch"
fetch "$BASE/download/$tag/checksums-team.txt" "$tmp/checksums.txt" || fail "could not download Team's checksums for $tag (is Team's signed build attached to this release yet?)"
key=${WERKBORD_TEAM_RELEASE_PUBLIC_KEY_FILE:-"$(dirname "$0")/keys/werkbord-team-release.pub"}
[ -f "$key" ] || fail "a trusted vendor release verification public key is required; obtain it independently and set WERKBORD_TEAM_RELEASE_PUBLIC_KEY_FILE (never download it from this release)"
command -v openssl >/dev/null 2>&1 || fail "OpenSSL with Ed25519 support is required to authenticate Team releases"
fetch "$BASE/download/$tag/checksums-team.txt.sig" "$tmp/checksums.txt.sig" || fail "the release has no signed manifest; nothing was installed"
# Bind the manifest to Team's archives and this release tag, not merely to a list of hashes.
printf 'werkbord-team/release/v1\000%s\000' "$tag" > "$tmp/signed-manifest"
cat "$tmp/checksums.txt" >> "$tmp/signed-manifest"
openssl pkeyutl -verify -pubin -inkey "$key" -rawin -in "$tmp/signed-manifest" -sigfile "$tmp/checksums.txt.sig" >/dev/null 2>&1 || fail "the release manifest signature is not valid (OpenSSL must support Ed25519); nothing was installed"
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
# The executable is published last. An interruption while sidecars are replaced
# leaves the old executable, which refuses a mismatched pin when next started.

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
mv -f "$dir/.werkbord-team.new" "$dir/werkbord-team"
say "Installed $dir/werkbord-team ($version)"
case ":$PATH:" in
  *":$dir:"*) ;;
  *) say "Note: $dir is not on your PATH. Add it to your shell profile:
  export PATH=\"$dir:\$PATH\"" ;;
esac
say ""
say "Next:"
say "  WERKBORD_TEAM_LICENSE_FILE=/secure/team-license.json werkbord-team workspace create --name \"Your team\" --owner \"Your name\""
say "  werkbord-team serve"
