#!/bin/sh
# Makes Sparkle available to the desktop build, and prints the directory that holds Sparkle.framework and
# bin/ (sign_update, generate_appcast, generate_keys). The version and the SHA-256 of its release archive are
# pinned in desktop/build/sparkle.env: what is downloaded is checked against that before anything is unpacked,
# and a directory that does not hold what it should is refused.
#
#   scripts/fetch-sparkle.sh            prints .cache/sparkle/<version>, downloading it the first time
#
# SPARKLE_DIR=… names a directory that already holds them (offline builds, tests); it is used as it is.
# SPARKLE_CACHE=… changes where downloads are kept (default .cache/sparkle, which git ignores).
set -eu

die() { printf 'fetch-sparkle: %s\n' "$*" >&2; exit 1; }
cd "$(dirname "$0")/.."
ROOT=$(pwd)

holds() { [ -d "$1/Sparkle.framework/Versions/B" ] && [ -x "$1/bin/sign_update" ] && [ -x "$1/bin/generate_appcast" ]; }

if [ -n "${SPARKLE_DIR:-}" ]; then
  holds "$SPARKLE_DIR" || die "$SPARKLE_DIR does not hold Sparkle.framework and bin/sign_update, bin/generate_appcast"
  printf '%s\n' "$SPARKLE_DIR"
  exit 0
fi

# shellcheck disable=SC1091
. desktop/build/sparkle.env
[ -n "${SPARKLE_VERSION:-}" ] && [ -n "${SPARKLE_SHA256:-}" ] && [ -n "${SPARKLE_URL:-}" ] || die "desktop/build/sparkle.env is incomplete"
dir="${SPARKLE_CACHE:-$ROOT/.cache/sparkle}/$SPARKLE_VERSION"
if holds "$dir"; then printf '%s\n' "$dir"; exit 0; fi

command -v curl >/dev/null 2>&1 && command -v tar >/dev/null 2>&1 || die "curl and tar are needed to fetch Sparkle"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM
echo "fetch-sparkle: downloading Sparkle $SPARKLE_VERSION" >&2
curl -fsSL --retry 3 --retry-delay 5 -o "$tmp/sparkle.tar.xz" "$SPARKLE_URL" || die "could not download $SPARKLE_URL"
have=$(shasum -a 256 "$tmp/sparkle.tar.xz" | awk '{print $1}')
[ "$have" = "$SPARKLE_SHA256" ] || die "the download's SHA-256 is $have, but desktop/build/sparkle.env pins $SPARKLE_SHA256: not using it"
mkdir -p "$tmp/x"
tar -xf "$tmp/sparkle.tar.xz" -C "$tmp/x"
holds "$tmp/x" || die "the archive does not hold Sparkle.framework and its tools"
mkdir -p "$(dirname "$dir")"
rm -rf "$dir.partial"
mv "$tmp/x" "$dir.partial"
mv "$dir.partial" "$dir"
printf '%s\n' "$dir"
