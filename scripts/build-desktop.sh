#!/bin/sh
# Builds the Werkbord desktop app for macOS: Werkbord.app, and with --package, a disk image
# (Werkbord_<version>_darwin_<arch>.dmg) with the app and a shortcut to /Applications.
#
#   scripts/build-desktop.sh [--package] [outdir]
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
#   ARCH               arm64 (default on Apple Silicon) or amd64 (Intel)
#   VERSION            what to stamp, e.g. v1.1.0. Default: what scripts/product.sh says a build of
#                      this working tree is (a clean checkout of a tag is exactly the release).
#   CODESIGN_IDENTITY  a "Developer ID Application: …" identity to sign for distribution (with the
#                      hardened runtime and a secure timestamp, as notarization needs). Default: "-",
#                      an ad-hoc signature, which is enough to run on the computer that built it.
#                      Notarization is a separate step this script does not do: docs/DESKTOP.md.
#   OUT                where to put the results (default dist/desktop)
set -eu

die() { echo "build-desktop: $*" >&2; exit 1; }

PACKAGE=""
OUT=${OUT:-dist/desktop}
for a in "$@"; do
  case "$a" in
    --package) PACKAGE=1 ;;
    -*) die "unknown option $a" ;;
    *) OUT=$a ;;
  esac
done

[ "$(uname -s)" = Darwin ] || die "the desktop app is built on macOS (it needs the system's web view and frameworks)"
cd "$(dirname "$0")/.."
ROOT=$(pwd)
for tool in go sips iconutil codesign plutil; do
  command -v "$tool" >/dev/null 2>&1 || die "$tool is needed (install the Xcode command line tools: xcode-select --install)"
done
[ -z "$PACKAGE" ] || command -v hdiutil >/dev/null 2>&1 || die "hdiutil is needed to make a disk image"
[ -f internal/webui/dist/index.html ] || die "the web app is not built: run 'make web web-embed' first"

case "${ARCH:-$(uname -m)}" in
  arm64|aarch64) ARCH=arm64; CARCH=arm64 ;;
  x86_64|amd64) ARCH=amd64; CARCH=x86_64 ;;
  *) die "no build for ${ARCH:-$(uname -m)}" ;;
esac

VERSION=${VERSION:-$(scripts/product.sh werkbord build-version)}
case "$VERSION" in v[0-9]*.[0-9]*.[0-9]*|dev) ;; *) die "VERSION \"$VERSION\" is not a version (v1.2.3)" ;; esac
# The bundle's own version is numbers only (the part before any "-" that says it is a build from source).
PLIST_VERSION=$(printf '%s' "${VERSION#v}" | sed 's/-.*//')
[ "$VERSION" != dev ] || PLIST_VERSION=0.0.0
IDENTITY=${CODESIGN_IDENTITY:--}

STAGE=$(mktemp -d)
trap 'rm -rf "$STAGE"' EXIT INT TERM
APP="$STAGE/Werkbord.app"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Helpers" "$APP/Contents/Resources"

echo "building Werkbord $VERSION for darwin/$ARCH"

# 1. The controller and command line: exactly what a release archive carries (scripts/build-release.sh).
CGO_ENABLED=0 GOOS=darwin GOARCH=$ARCH go build -trimpath -ldflags "-s -w -X main.version=$VERSION" \
  -o "$APP/Contents/Helpers/werkbord" ./cmd/werkbord

# 2. The window. It is its own module (desktop/go.mod) because it needs cgo and the system's web view.
#    UniformTypeIdentifiers is what Wails's file dialogs link against; the Wails command line adds it, so this must.
(
  cd desktop
  CGO_ENABLED=1 GOOS=darwin GOARCH=$ARCH \
  CGO_CFLAGS="-arch $CARCH -mmacosx-version-min=13.0" \
  CGO_LDFLAGS="-arch $CARCH -mmacosx-version-min=13.0 -framework UniformTypeIdentifiers" \
    go build -trimpath -tags desktop,production -ldflags "-s -w -X main.version=$VERSION" \
    -o "$APP/Contents/MacOS/Werkbord" .
)

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

# 5. Sign. Inside-out: the helper first, then the app that holds it. With a real identity this is the
#    hardened runtime with a secure timestamp, which is what notarization requires; ad hoc is for this computer.
if [ "$IDENTITY" = "-" ]; then
  codesign --force --sign - "$APP/Contents/Helpers/werkbord"
  codesign --force --sign - --entitlements desktop/build/darwin/entitlements.plist "$APP"
else
  codesign --force --options runtime --timestamp --sign "$IDENTITY" "$APP/Contents/Helpers/werkbord"
  codesign --force --options runtime --timestamp --sign "$IDENTITY" --entitlements desktop/build/darwin/entitlements.plist "$APP"
fi
codesign --verify --strict --deep "$APP" || die "the signed app does not verify"

# 6. The helper must say the version the app does: the app installs it, and an update compares against it.
if [ "$ARCH" = "$(uname -m | sed 's/aarch64/arm64/; s/x86_64/amd64/')" ]; then
  reported=$("$APP/Contents/Helpers/werkbord" version)
  [ "$reported" = "$VERSION" ] || die "the program inside the app says \"$reported\", not $VERSION"
fi

# Only what this script makes is replaced: OUT may be a directory with other things in it.
mkdir -p "$OUT"
OUT=$(cd "$OUT" && pwd)
rm -rf "$OUT/Werkbord.app"
ditto "$APP" "$OUT/Werkbord.app" # ditto keeps the signature, permissions and symlinks that cp can lose
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
  until hdiutil create -quiet -volname "Werkbord" -srcfolder "$DISK" -fs HFS+ -format UDZO -ov "$OUT/$DMG"; do
    tries=$((tries + 1))
    [ $tries -lt 4 ] || die "hdiutil could not make the disk image"
    sleep 3
  done
  if [ "$IDENTITY" != "-" ]; then codesign --force --timestamp --sign "$IDENTITY" "$OUT/$DMG"; fi
  (cd "$OUT" && shasum -a 256 "$DMG" > "$DMG.sha256")
  echo "wrote $OUT/$DMG ($(du -h "$OUT/$DMG" | cut -f1))"
  [ "$IDENTITY" != "-" ] || echo "note: signed ad hoc. Someone else's Mac will warn that it cannot check the app until it is signed with a Developer ID and notarized (docs/DESKTOP.md)."
fi
