#!/bin/sh
# Checks the feed the app updates itself from, against the update archive it points at, as published.
#
#   scripts/verify-appcast.sh <werkbord-vX.Y.Z> [appcast.xml]
#
# It downloads the release's update archive (Werkbord_<version>_darwin_<arch>.zip) and its .sha256 from the release page,
# and checks the archive against that checksum. Then it takes the appcast (the file given, or the release's own
# appcast.xml) and, with the public key and the version it reads out of the app INSIDE the archive (that is what the
# installed app will hold), checks everything an installed app will hold the feed to (go run ./scripts/appcast check):
# one item; this release's version; https, under this release's download address; the archive's length; an EdDSA
# signature that verifies; the feed itself signed. If the release is the newest individual one it then downloads the feed
# from the address the apps use and checks it is the same file. Anything wrong is a failure with the reason.
#
# It needs curl, unzip, python3 and Go, and nothing from Apple, so it runs on the Linux job that publishes the feed
# before it publishes it, and by hand. (WERKBORD_FEED_URL=none skips the comparison with the live feed, which only the newest
# individual release's can pass.) Environment: WERKBORD_RELEASE_BASE (default GitHub's download address),
# DESKTOP_ARCH (default universal), WERKBORD_FEED_URL (the address the apps use; "none" skips that last check).
set -eu

die() { printf 'verify-appcast: FAILED: %s\n' "$*" >&2; exit 1; }
say() { printf 'verify-appcast: %s\n' "$*"; }

tag=${1:-}
case "$tag" in
  werkbord-team-v*) die "$tag is a Werkbord Team release: the Mac app belongs to the individual product" ;;
  werkbord-v[0-9]*.[0-9]*.[0-9]*) ;;
  *) die "usage: verify-appcast.sh werkbord-vX.Y.Z [appcast.xml]" ;;
esac
version=${tag#werkbord-v}
BASE=${WERKBORD_RELEASE_BASE:-https://github.com/micho8cho93/werkbord/releases/download}
ARCH=${DESKTOP_ARCH:-universal}
FEED=${WERKBORD_FEED_URL:-https://github.com/micho8cho93/werkbord/releases/latest/download/appcast.xml}
cd "$(dirname "$0")/.."
for tool in curl unzip python3 go; do command -v "$tool" >/dev/null 2>&1 || die "$tool is needed"; done
case "$BASE" in file://*) RETRY="" ;; *) RETRY="--retry 6 --retry-delay 10 --retry-all-errors" ;; esac

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT INT TERM
zip="Werkbord_${version}_darwin_${ARCH}.zip"
# shellcheck disable=SC2086
curl -fsSL $RETRY -o "$work/$zip" "$BASE/$tag/$zip" || die "the release has no update archive $zip"
# shellcheck disable=SC2086
curl -fsSL $RETRY -o "$work/$zip.sha256" "$BASE/$tag/$zip.sha256" || die "the release has no checksum for $zip"
want=$(awk '{print $1}' "$work/$zip.sha256")
if command -v sha256sum >/dev/null 2>&1; then have=$(sha256sum "$work/$zip" | awk '{print $1}'); else have=$(shasum -a 256 "$work/$zip" | awk '{print $1}'); fi
[ -n "$want" ] && [ "$want" = "$have" ] || die "the update archive's sha256 is $have, the release says ${want:-nothing}"
say "ok  the update archive matches its checksum"

if [ -n "${2:-}" ]; then
  cp "$2" "$work/appcast.xml"
else
  # shellcheck disable=SC2086
  curl -fsSL $RETRY -o "$work/appcast.xml" "$BASE/$tag/appcast.xml" || die "the release has no appcast.xml"
fi

unzip -p "$work/$zip" 'Werkbord.app/Contents/Info.plist' > "$work/Info.plist" 2>/dev/null || die "$zip does not hold Werkbord.app/Contents/Info.plist"
pub=$(python3 -c 'import plistlib,sys; print(plistlib.load(open(sys.argv[1],"rb")).get("SUPublicEDKey",""))' "$work/Info.plist")
bundle=$(python3 -c 'import plistlib,sys; print(plistlib.load(open(sys.argv[1],"rb")).get("CFBundleVersion",""))' "$work/Info.plist")
[ -n "$pub" ] || die "the app in $zip has no SUPublicEDKey: it could not verify any update"
keyfile=${SPARKLE_PUBLIC_KEY_FILE:-desktop/build/darwin/sparkle-public-key}
if [ -f "$keyfile" ] && [ "$(tr -d '[:space:]' < "$keyfile")" != "$pub" ]; then
  die "the app in $zip holds a different update key than desktop/build/darwin/sparkle-public-key: installed apps would refuse this update"
fi
go run ./scripts/appcast check -tag "$tag" -pubkey "$pub" -archive "$work/$zip" -bundle-version "$bundle" "$work/appcast.xml" || die "the appcast is not what an installed app will accept"
say "ok  the appcast is the release's, signed, and its archive verifies with the key the app holds"

if [ "$FEED" != none ]; then
  # shellcheck disable=SC2086
  if curl -fsSL $RETRY -o "$work/live.xml" "$FEED"; then
    cmp -s "$work/live.xml" "$work/appcast.xml" && say "ok  $FEED serves exactly this appcast" ||
      die "$FEED serves a different appcast than this release's (the newest individual release's appcast is the one apps are given)"
  else
    die "$FEED cannot be downloaded: installed apps would find no update (is this release marked latest, and is appcast.xml among its assets?)"
  fi
fi
say "$tag's update is good"
