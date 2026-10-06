#!/bin/sh
# Makes the pinned Nebula release available to a Werkbord Team build, and prints the directory that holds the
# `nebula` program for one platform. The version, the archive names and every SHA-256 are pinned in
# internal/team/infra/nebula/manifest.go (the same file the running server checks the binary against): what is
# downloaded is checked against the archive's pin before anything is unpacked, and the unpacked program against
# its own pin, and anything that does not match is refused. "Latest" is never fetched.
#
#   scripts/fetch-nebula.sh                 prints .cache/nebula/<version>/<os>_<arch> for this computer
#   scripts/fetch-nebula.sh linux arm64     the same for another platform (a release build)
#
# NEBULA_DIR=…   names a directory that already holds `nebula` (offline builds, tests); it is still checked
#                against the pin, and used as it is.
# NEBULA_CACHE=… changes where downloads are kept (default .cache/nebula, which git ignores).
set -eu

die() { printf 'fetch-nebula: %s\n' "$*" >&2; exit 1; }
cd "$(dirname "$0")/.."
ROOT=$(pwd)
MANIFEST=internal/team/infra/nebula/manifest.go

if command -v sha256sum >/dev/null 2>&1; then sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
else die "sha256sum or shasum is needed"; fi

os=${1:-$(uname -s | tr '[:upper:]' '[:lower:]')}
arch=${2:-$(uname -m)}
case "$arch" in x86_64) arch=amd64 ;; aarch64) arch=arm64 ;; esac

version=$(sed -n 's/^const Version = "\(.*\)"$/\1/p' "$MANIFEST")
release=$(sed -n 's/^const ReleaseURL = "\(.*\)" + Version + "\/"$/\1/p' "$MANIFEST")
line=$(grep -E "^[[:space:]]*\"$os/$arch\":" "$MANIFEST" || true)
[ -n "$version" ] && [ -n "$release" ] || die "$MANIFEST is not what this script expects"
[ -n "$line" ] || die "no pinned Nebula release for $os/$arch (see $MANIFEST)"
field() { printf '%s' "$line" | sed -n "s/.*$1: \"\\([^\"]*\\)\".*/\\1/p"; }
archive=$(field Archive); archive_sha=$(field ArchiveSHA256); binary_sha=$(field BinarySHA256)
[ -n "$archive" ] && [ -n "$archive_sha" ] && [ -n "$binary_sha" ] || die "the pin for $os/$arch is incomplete"

check_binary() { [ -f "$1/nebula" ] && [ "$(sha256 "$1/nebula")" = "$binary_sha" ]; }

if [ -n "${NEBULA_DIR:-}" ]; then
  check_binary "$NEBULA_DIR" || die "$NEBULA_DIR/nebula is not the pinned Nebula $version for $os/$arch"
  printf '%s\n' "$NEBULA_DIR"; exit 0
fi

dir="${NEBULA_CACHE:-$ROOT/.cache/nebula}/$version/${os}_${arch}"
if check_binary "$dir"; then printf '%s\n' "$dir"; exit 0; fi

command -v curl >/dev/null 2>&1 || die "curl is needed to fetch Nebula"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM
echo "fetch-nebula: downloading Nebula $version for $os/$arch" >&2
curl -fsSL --retry 3 --retry-delay 5 -o "$tmp/$archive" "${release}${version}/$archive" || die "could not download $archive"
[ "$(sha256 "$tmp/$archive")" = "$archive_sha" ] || die "the download's SHA-256 is $(sha256 "$tmp/$archive"), but the pin is $archive_sha: not using it"
mkdir -p "$tmp/x"
case "$archive" in
  *.zip) command -v unzip >/dev/null 2>&1 || die "unzip is needed"; unzip -q -o "$tmp/$archive" nebula -d "$tmp/x" ;;
  *.tar.gz) tar -xzf "$tmp/$archive" -C "$tmp/x" nebula ;;
  *) die "unknown archive type $archive" ;;
esac
check_binary "$tmp/x" || die "the program in $archive is not the pinned one"
chmod 0755 "$tmp/x/nebula"
mkdir -p "$(dirname "$dir")"
rm -rf "$dir.partial"
mv "$tmp/x" "$dir.partial"
mv "$dir.partial" "$dir"
printf '%s\n' "$dir"
