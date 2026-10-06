#!/bin/sh
# Builds the Werkbord desktop app for macOS: Werkbord.app, and with --package, a disk image
# (Werkbord_<version>_darwin_<arch>.dmg) with the app and a shortcut to /Applications.
#
#   scripts/build-desktop.sh [--package] [--release] [--notarize] [outdir]
#
# What goes in the app (docs/DESKTOP.md):
#
#   Contents/MacOS/Werkbord        the window (desktop/, Wails)
#   Contents/Helpers/werkbord      the controller and command line, the same program the release
#                                  archives and the installer carry, with the web app embedded
#   Contents/Resources/icon.icns   made from desktop/build/appicon.png
#
# Codex and Claude Code are not in it: Werkbord finds the ones the person has installed.
#
# The web app must already be built and embedded (make web web-embed). Environment:
#   ARCH               arm64 or amd64 (default: this Mac's), or universal: both in one app, which is what
#                      a release is (a person should not have to know which chip they have). The window
#                      and the program are built for each and joined with lipo; the disk image is
#                      Werkbord_<version>_darwin_universal.dmg.
#   VERSION            what to stamp, e.g. v1.1.0. Default: what scripts/product.sh says a build of
#                      this working tree is (a clean checkout of a tag is exactly the release).
#   CODESIGN_IDENTITY  a "Developer ID Application: …" identity to sign for distribution. Default: "-",
#                      an ad-hoc signature, which is enough to run on the computer that built it.
#                      A real identity turns on the hardened runtime and a secure timestamp, which is
#                      what notarization requires.
#   CODESIGN_KEYCHAIN  the keychain that holds the identity (CI imports it into a temporary one and
#                      never touches the login keychain). Default: the keychains the user has.
#   OUT                where to put the results (default dist/desktop)
#
# --release (or RELEASE=1) is the mode that makes what is published. It refuses to build unless
# the result could be distributed: a real Developer ID identity (never ad hoc), a secure timestamp, the
# hardened runtime, a version that is exactly a release (v1.2.3), the universal build, and it checks all of
# that on what it signed. It also notarizes (below) and fails if it cannot. Nothing that comes out of it is ad hoc or
# un-notarized, so an unsigned disk image cannot be published by mistake. Without it the build is for this computer.
#
# --notarize (implied by --release) sends the signed app to Apple, staples the ticket, and does it again for
# the disk image (scripts/notarize-desktop.sh, which has the credentials: NOTARY_KEY_FILE, NOTARY_KEY_ID,
# NOTARY_ISSUER, and says why it does the app before the image). Without them a build that is not a release
# says so and goes on, un-notarized.
#
# Test hooks, not reachable in release mode (the script stops if they are set there):
#   CODESIGN_TIMESTAMP=none   sign without contacting Apple's timestamp server (offline tests)
#   CODESIGN=…, HDIUTIL=…     the programs it runs (tests substitute recorders; also XCRUN, SPCTL for the notarization)
set -eu

die() { echo "build-desktop: $*" >&2; exit 1; }

PACKAGE=""
NOTARIZE=${NOTARIZE:-}
RELEASE=${RELEASE:-}
OUT=${OUT:-dist/desktop}
for a in "$@"; do
  case "$a" in
    --package) PACKAGE=1 ;;
    --release) RELEASE=1 ;;
    --notarize) NOTARIZE=1 ;;
    -*) die "unknown option $a" ;;
    *) OUT=$a ;;
  esac
done
[ "$RELEASE" != 0 ] || RELEASE=""
[ "$NOTARIZE" != 0 ] || NOTARIZE=""
# What the notarization step is told when it finds no credentials: a release stops, anything else says so and goes on.
REQUIRE=""
if [ -n "$RELEASE" ]; then NOTARIZE=1; REQUIRE=--require; fi

[ "$(uname -s)" = Darwin ] || die "the desktop app is built on macOS (it needs the system's web view and frameworks)"
cd "$(dirname "$0")/.."
ROOT=$(pwd)
CODESIGN=${CODESIGN:-codesign}
HDIUTIL=${HDIUTIL:-hdiutil}
for tool in go sips iconutil plutil "$CODESIGN"; do
  command -v "$tool" >/dev/null 2>&1 || die "$tool is needed (install the Xcode command line tools: xcode-select --install)"
done
[ -z "$PACKAGE" ] || command -v "$HDIUTIL" >/dev/null 2>&1 || die "hdiutil is needed to make a disk image"
[ -f internal/webui/dist/index.html ] || die "the web app is not built: run 'make web web-embed' first"

# What a release is built for is both kinds of Mac; anything else defaults to the Mac that is building.
HOST=$(uname -m | sed 's/aarch64/arm64/; s/x86_64/amd64/')
if [ -n "$RELEASE" ] && [ -z "${ARCH:-}" ]; then ARCH=universal; fi
case "${ARCH:-$HOST}" in
  arm64|aarch64) ARCH=arm64; ARCHS="arm64" ;;
  x86_64|amd64) ARCH=amd64; ARCHS="amd64" ;;
  universal) ARCH=universal; ARCHS="arm64 amd64"; command -v lipo >/dev/null 2>&1 || die "lipo is needed to join the two builds (install the Xcode command line tools: xcode-select --install)" ;;
  *) die "no build for ${ARCH:-$HOST}" ;;
esac
carch() { case $1 in amd64) echo x86_64 ;; *) echo "$1" ;; esac; } # the name clang and lipo use

VERSION=${VERSION:-$(scripts/product.sh werkbord build-version)}
case "$VERSION" in v[0-9]*.[0-9]*.[0-9]*|dev) ;; *) die "VERSION \"$VERSION\" is not a version (v1.2.3)" ;; esac
# The bundle's own version is numbers only (the part before any "-" that says it is a build from source).
PLIST_VERSION=$(printf '%s' "${VERSION#v}" | sed 's/-.*//')
[ "$VERSION" != dev ] || PLIST_VERSION=0.0.0
IDENTITY=${CODESIGN_IDENTITY:--}
TIMESTAMP=${CODESIGN_TIMESTAMP:-}
ENTITLEMENTS=desktop/build/darwin/entitlements.plist

# ---- what a release build insists on, before anything is built ----
if [ -n "$RELEASE" ]; then
  [ "$IDENTITY" != "-" ] && [ -n "$IDENTITY" ] ||
    die "a release build must be signed with a Developer ID Application identity, never ad hoc: set CODESIGN_IDENTITY (docs/DESKTOP_RELEASE.md, steps 2-3 and 5)"
  [ -z "$TIMESTAMP" ] || die "CODESIGN_TIMESTAMP is for tests only: a release is signed with Apple's secure timestamp"
  [ "$CODESIGN" = codesign ] || die "CODESIGN is for tests only: a release is signed with the system's codesign"
  [ "$HDIUTIL" = hdiutil ] || die "HDIUTIL is for tests only"
  [ -z "${XCRUN:-}${SPCTL:-}${DITTO:-}" ] || die "XCRUN, SPCTL and DITTO are for tests only: a release is notarized with Apple's own tools"
  [ "$ARCH" = universal ] || die "a release is one universal disk image (ARCH=universal), so that nobody has to know their chip; not \"$ARCH\""
  printf '%s' "$VERSION" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$' ||
    die "a release is built from a version that is exactly a release (v1.2.3), not \"$VERSION\""
  [ "$VERSION" = "v$(scripts/product.sh werkbord version)" ] ||
    die "VERSION $VERSION is not what cmd/werkbord/VERSION says (v$(scripts/product.sh werkbord version))"
  # A release is notarized, so it must be able to be: found out now and not after the build.
  [ -n "${NOTARY_KEY_FILE:-}" ] && [ -n "${NOTARY_KEY_ID:-}" ] && [ -n "${NOTARY_ISSUER:-}" ] ||
    die "a release is notarized: NOTARY_KEY_FILE, NOTARY_KEY_ID and NOTARY_ISSUER must all be set (docs/DESKTOP_RELEASE.md, steps 4-5)"
else
  case "$TIMESTAMP" in ""|none) ;; *) die "CODESIGN_TIMESTAMP can only be \"none\"" ;; esac
  [ -z "$NOTARIZE" ] || [ "$IDENTITY" != "-" ] || die "--notarize needs a Developer ID identity: Apple does not notarize an ad hoc signature"
  [ "$IDENTITY" = "-" ] || [ "$TIMESTAMP" != none ] || echo "build-desktop: TEST ONLY: signing without a secure timestamp. Do not distribute this build." >&2
fi

STAGE=$(mktemp -d)
trap 'rm -rf "$STAGE"' EXIT INT TERM
APP="$STAGE/Werkbord.app"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Helpers" "$APP/Contents/Resources"

echo "building Werkbord $VERSION for darwin/$ARCH"

# 1. The controller and command line: exactly what a release archive carries (scripts/build-release.sh).
# 2. The window. It is its own module (desktop/go.mod) because it needs cgo and the system's web view.
#    UniformTypeIdentifiers is what Wails's file dialogs link against; the Wails command line adds it, so this must.
# Each for every architecture asked for, then joined: a universal Mach-O holds one complete build of each, and the
# system runs the one that fits the Mac. (The Go toolchain cross-builds either way, and clang's -arch does the same for cgo.)
HELPERS=""; WINDOWS=""
for a in $ARCHS; do
  c=$(carch "$a")
  CGO_ENABLED=0 GOOS=darwin GOARCH=$a go build -trimpath -ldflags "-s -w -X main.version=$VERSION" \
    -o "$STAGE/helper-$a" ./cmd/werkbord
  (
    cd desktop
    CGO_ENABLED=1 GOOS=darwin GOARCH=$a \
    CGO_CFLAGS="-arch $c -mmacosx-version-min=13.0" \
    CGO_LDFLAGS="-arch $c -mmacosx-version-min=13.0 -framework UniformTypeIdentifiers" \
      go build -trimpath -tags desktop,production -ldflags "-s -w -X main.version=$VERSION" \
      -o "$STAGE/window-$a" .
  )
  HELPERS="$HELPERS $STAGE/helper-$a"; WINDOWS="$WINDOWS $STAGE/window-$a"
done
if [ "$ARCH" = universal ]; then
  # shellcheck disable=SC2086
  lipo -create -output "$APP/Contents/Helpers/werkbord" $HELPERS
  # shellcheck disable=SC2086
  lipo -create -output "$APP/Contents/MacOS/Werkbord" $WINDOWS
else
  cp "$STAGE/helper-$ARCH" "$APP/Contents/Helpers/werkbord"
  cp "$STAGE/window-$ARCH" "$APP/Contents/MacOS/Werkbord"
fi
chmod 755 "$APP/Contents/Helpers/werkbord" "$APP/Contents/MacOS/Werkbord"
rm -f $HELPERS $WINDOWS

# 3. The icon, from the one 1024px picture (Apple's own tools; nothing to install).
ICONSET="$STAGE/icon.iconset"
mkdir -p "$ICONSET"
for s in 16 32 128 256 512; do
  sips -z $s $s desktop/build/appicon.png --out "$ICONSET/icon_${s}x${s}.png" >/dev/null
  sips -z $((s * 2)) $((s * 2)) desktop/build/appicon.png --out "$ICONSET/icon_${s}x${s}@2x.png" >/dev/null
done
iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/icon.icns"

# 4. The bundle's description.
sed "s/@VERSION@/$PLIST_VERSION/g" desktop/build/darwin/Info.plist > "$APP/Contents/Info.plist"
plutil -lint "$APP/Contents/Info.plist" >/dev/null
printf 'APPL????' > "$APP/Contents/PkgInfo"

# ---- signing ----
#
# sign_code <path> [entitlements]: one piece of code. A real identity gets the hardened runtime
# (which notarization requires) and Apple's secure timestamp (so the signature outlives the
# certificate); ad hoc is for this computer and is neither.
sign_code() {
  _path=$1
  _entitlements=${2:-}
  set -- --force --sign "$IDENTITY"
  [ -z "${CODESIGN_KEYCHAIN:-}" ] || set -- "$@" --keychain "$CODESIGN_KEYCHAIN"
  if [ "$IDENTITY" != "-" ]; then
    set -- "$@" --options runtime
    if [ "$TIMESTAMP" = none ]; then set -- "$@" --timestamp=none; else set -- "$@" --timestamp; fi
  fi
  [ -z "$_entitlements" ] || set -- "$@" --entitlements "$_entitlements"
  "$CODESIGN" "$@" "$_path" || die "could not sign $_path"
}

# sign_bundle: inside-out. Code signs what it contains by hash, so whatever is inside must be signed
# before the thing that holds it, or the outer signature seals a stale hash and the app fails to verify.
# Each piece is signed by name, and nothing with --deep, which signs everything the same way and hides
# what was not meant to be there; check_bundle below then proves nothing was missed.
sign_bundle() {
  app=$1
  sign_code "$app/Contents/Helpers/werkbord"
  sign_code "$app" "$ENTITLEMENTS"
}

# check_bundle <app|dmg>: the signature is not just present but what a distributed build needs. The
# policy (every piece of code signed, by the Developer ID, with the hardened runtime and a timestamp) is
# one script, shared with the tests and with the check of a published release.
check_bundle() {
  if [ "$IDENTITY" = "-" ]; then mode=--adhoc; else mode=--distribution; fi
  [ "$TIMESTAMP" != none ] || mode="$mode --no-timestamp"
  # shellcheck disable=SC2086
  CODESIGN="$CODESIGN" scripts/check-desktop-signature.sh $mode "$1" >/dev/null || die "the signature of $1 is not what a build with this identity must have"
}

# 5. Sign, and check what was signed.
sign_bundle "$APP"
check_bundle "$APP"

# 5b. Notarize and staple the app itself, before it goes into the disk image: the ticket stapled to an image is not carried
# to an app that is dragged out of it (scripts/notarize-desktop.sh says why that matters).
if [ -n "$NOTARIZE" ]; then
  scripts/notarize-desktop.sh $REQUIRE app "$APP" || die "the app was not notarized"
  check_bundle "$APP" # stapling must not have disturbed the signature
fi

# 6. The helper must say the version the app does: the app installs it, and an update compares against it.
if [ "$ARCH" = "$HOST" ] || [ "$ARCH" = universal ]; then
  # With a time limit: a program that is not the one it should be (say, the window) would otherwise sit there forever.
  reported=$(perl -e 'alarm 30; exec @ARGV' "$APP/Contents/Helpers/werkbord" version) || die "the program inside the app does not report its version"
  [ "$reported" = "$VERSION" ] || die "the program inside the app says \"$reported\", not $VERSION"
fi

# Only what this script makes is replaced: OUT may be a directory with other things in it.
mkdir -p "$OUT"
OUT=$(cd "$OUT" && pwd)
rm -rf "$OUT/Werkbord.app"
ditto "$APP" "$OUT/Werkbord.app" # ditto keeps the signature, permissions and symlinks that cp can lose
check_bundle "$OUT/Werkbord.app"
echo "wrote $OUT/Werkbord.app"

# 7. The disk image: the app, and a shortcut to drag it onto.
if [ -n "$PACKAGE" ]; then
  DMG="Werkbord_${VERSION#v}_darwin_${ARCH}.dmg"
  DISK="$STAGE/disk"
  mkdir -p "$DISK"
  ditto "$APP" "$DISK/Werkbord.app"
  ln -s /Applications "$DISK/Applications"
  rm -f "$OUT/$DMG" "$OUT/$DMG.sha256"
  # hdiutil sometimes fails with "Resource busy" on a machine that has just been writing the same files (CI runners).
  tries=0
  until "$HDIUTIL" create -quiet -volname "Werkbord" -srcfolder "$DISK" -fs HFS+ -format UDZO -ov "$OUT/$DMG"; do
    tries=$((tries + 1))
    [ $tries -lt 4 ] || die "hdiutil could not make the disk image"
    sleep 3
  done
  if [ "$IDENTITY" != "-" ]; then
    # The disk image is signed too (no hardened runtime: it is not code), so that Gatekeeper can say who made it.
    set -- --force --sign "$IDENTITY"
    [ -z "${CODESIGN_KEYCHAIN:-}" ] || set -- "$@" --keychain "$CODESIGN_KEYCHAIN"
    if [ "$TIMESTAMP" = none ]; then set -- "$@" --timestamp=none; else set -- "$@" --timestamp; fi
    "$CODESIGN" "$@" "$OUT/$DMG" || die "could not sign $OUT/$DMG"
    check_bundle "$OUT/$DMG"
  fi
  if [ -n "$NOTARIZE" ]; then
    scripts/notarize-desktop.sh $REQUIRE dmg "$OUT/$DMG" || die "the disk image was not notarized"
    check_bundle "$OUT/$DMG"
  fi
  # The checksum is of the finished file: stapling changes it.
  (cd "$OUT" && shasum -a 256 "$DMG" > "$DMG.sha256")
  echo "wrote $OUT/$DMG ($(du -h "$OUT/$DMG" | cut -f1))"
  if [ "$IDENTITY" = "-" ]; then
    echo "note: signed ad hoc. Someone else's Mac will warn that it cannot check the app until it is signed with a Developer ID and notarized (docs/DESKTOP.md)."
  elif [ -z "$NOTARIZE" ]; then
    echo "note: signed, not notarized. Someone else's Mac will warn that it cannot check the app (--notarize, or --release)."
  fi
fi
