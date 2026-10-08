#!/bin/sh
# Separate macOS Team app. Nothing is installed on the build machine.
# Usage: scripts/build-team-desktop.sh [--package] [--release] [outdir]
# A release requires Developer ID/notary credentials and LICENSE_ISSUER_PUBLIC_KEY (Ed25519, raw URL base64).
# ARCH=universal is the distribution default; native builds are useful for development.
set -eu
cd "$(dirname "$0")/.."
ROOT=$(pwd)
die() { printf 'build-team-desktop: %s\n' "$*" >&2; exit 1; }
[ "$(uname -s)" = Darwin ] || die "the Team desktop packager requires macOS"
PACKAGE=""; RELEASE=""; OUT=${OUT:-dist/team-desktop}
while [ $# -gt 0 ]; do
  case "$1" in --package) PACKAGE=1 ;; --release) RELEASE=1; PACKAGE=1 ;; --*) die "unknown option: $1" ;; *) OUT=$1 ;; esac
  shift
done
ARCH=${ARCH:-universal}
case "$ARCH" in universal) ARCHS="arm64 amd64" ;; arm64|amd64) ARCHS=$ARCH ;; *) die "ARCH must be universal, arm64 or amd64" ;; esac
VERSION=${VERSION:-$(scripts/product.sh werkbord-team build-version)}
RUNNER_VERSION=$(scripts/product.sh werkbord build-version)
PLIST_VERSION=$(scripts/product.sh werkbord-team version)
IDENTITY=${CODESIGN_IDENTITY:--}
ISSUER=${LICENSE_ISSUER_PUBLIC_KEY:-}
if [ -n "$ISSUER" ]; then
  [ ${#ISSUER} -eq 43 ] || die "the issuer public key must encode 32 bytes"
  case "$ISSUER" in *[!A-Za-z0-9_-]*) die "the issuer public key must use raw URL base64" ;; esac
fi
if [ -n "$RELEASE" ]; then
  case "$IDENTITY" in 'Developer ID Application: '*) ;; *) die "a release requires a Developer ID Application identity" ;; esac
  [ "$ARCH" = universal ] || die "a release is universal so users do not need to choose their chip"
  [ "$VERSION" = "v$PLIST_VERSION" ] || die "release VERSION must match cmd/werkbord-team/VERSION"
  [ -n "$ISSUER" ] || die "a release requires LICENSE_ISSUER_PUBLIC_KEY; no test issuer is shipped"
  [ -n "${NOTARY_KEY_FILE:-}" ] && [ -n "${NOTARY_KEY_ID:-}" ] && [ -n "${NOTARY_ISSUER:-}" ] || die "a release requires the Apple notary credentials"
  [ -z "${CODESIGN_TIMESTAMP:-}${XCRUN:-}${SPCTL:-}${DITTO:-}" ] || die "test tool overrides cannot be used for a release"
fi
STAGE=$(mktemp -d)
trap 'rm -rf "$STAGE"' EXIT INT TERM
APP="$STAGE/Werkbord Team.app"
H="$APP/Contents/Helpers"
mkdir -p "$H" "$APP/Contents/MacOS" "$APP/Contents/Resources/licenses"
sign() {
  set -- --force --sign "$IDENTITY" "$1"
  [ -z "${CODESIGN_KEYCHAIN:-}" ] || set -- "$@" --keychain "$CODESIGN_KEYCHAIN"
  [ "$IDENTITY" = - ] || set -- "$@" --options runtime --timestamp
  /usr/bin/codesign "$@"
}
# Fetch verifies the upstream hash/source commit BEFORE combining/signing. Nebula retains its upstream signature and pin.
NEBULA=$(scripts/fetch-nebula.sh darwin arm64)
cp "$NEBULA/nebula" "$H/nebula"
DATABASES=""
for a in $ARCHS; do
  RQLITE=$(scripts/fetch-rqlite.sh darwin "$a")
  cp "$RQLITE/rqlited" "$STAGE/database-$a"
  DATABASES="$DATABASES $STAGE/database-$a"
done
if [ "$ARCH" = universal ]; then
  # shellcheck disable=SC2086
  lipo -create -output "$H/rqlited" $DATABASES
else cp "$STAGE/database-$ARCH" "$H/rqlited"; fi
sign "$H/rqlited"
DATABASE_SHA=$(shasum -a 256 "$H/rqlited" | cut -d' ' -f1)
RQLITE_VERSION=$(sed -n 's/^const Version = "\(.*\)"$/\1/p' internal/team/infra/rqlite/manifest.go)
RQLITE_COMMIT=$(sed -n 's/^const SourceCommit = "\(.*\)"$/\1/p' internal/team/infra/rqlite/manifest.go)
printf '{"version":"v%s","commit":"%s","sha256":"%s"}\n' "$RQLITE_VERSION" "$RQLITE_COMMIT" "$DATABASE_SHA" > "$APP/Contents/Resources/rqlited.build"
# Only the free runner's web build is used here. Its binary is a separate, optional helper with its own version.
[ -f internal/webui/dist/index.html ] || die "build the optional free runner's UI first with make web web-embed"
TEAM_PROGRAMS=""; RUNNERS=""; WINDOWS=""
for a in $ARCHS; do
  case "$a" in arm64) C_ARCH=arm64 ;; amd64) C_ARCH=x86_64 ;; esac
  CGO_ENABLED=1 GOOS=darwin GOARCH=$a CGO_CFLAGS="-arch $C_ARCH -mmacosx-version-min=13.0" CGO_LDFLAGS="-arch $C_ARCH -mmacosx-version-min=13.0" go build -trimpath \
    -ldflags "-s -w -X main.version=$VERSION -X main.licenseIssuer=$ISSUER -X devboard/internal/team/infra/rqlite.distributionBinarySHA256=$DATABASE_SHA" \
    -o "$STAGE/team-$a" ./cmd/werkbord-team
  CGO_ENABLED=0 GOOS=darwin GOARCH=$a go build -trimpath -ldflags "-s -w -X main.version=$RUNNER_VERSION" -o "$STAGE/runner-$a" ./cmd/werkbord
  (
    cd cmd/werkbord-team/desktop
    CGO_ENABLED=1 GOOS=darwin GOARCH=$a \
      CGO_CFLAGS="-arch $C_ARCH -mmacosx-version-min=13.0" \
      CGO_LDFLAGS="-arch $C_ARCH -mmacosx-version-min=13.0 -framework UniformTypeIdentifiers" \
      go build -trimpath -tags desktop,production -ldflags "-s -w -X main.version=$VERSION" -o "$STAGE/window-$a" .
  )
  TEAM_PROGRAMS="$TEAM_PROGRAMS $STAGE/team-$a"; RUNNERS="$RUNNERS $STAGE/runner-$a"; WINDOWS="$WINDOWS $STAGE/window-$a"
done
if [ "$ARCH" = universal ]; then
  # shellcheck disable=SC2086
  lipo -create -output "$H/werkbord-team" $TEAM_PROGRAMS
  # shellcheck disable=SC2086
  lipo -create -output "$H/werkbord" $RUNNERS
  # shellcheck disable=SC2086
  lipo -create -output "$APP/Contents/MacOS/Werkbord Team" $WINDOWS
else
  cp "$STAGE/team-$ARCH" "$H/werkbord-team"
  cp "$STAGE/runner-$ARCH" "$H/werkbord"
  cp "$STAGE/window-$ARCH" "$APP/Contents/MacOS/Werkbord Team"
fi
chmod 755 "$H/werkbord-team" "$H/werkbord" "$H/rqlited" "$H/nebula" "$APP/Contents/MacOS/Werkbord Team"
ICONSET="$STAGE/icon.iconset"
mkdir -p "$ICONSET"
for s in 16 32 128 256 512; do
  sips -z "$s" "$s" desktop/build/appicon.png --out "$ICONSET/icon_${s}x${s}.png" >/dev/null
  sips -z "$((s*2))" "$((s*2))" desktop/build/appicon.png --out "$ICONSET/icon_${s}x${s}@2x.png" >/dev/null
done
iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/icon.icns"
sed "s/VERSION/$PLIST_VERSION/g" cmd/werkbord-team/desktop/build/darwin/Info.plist > "$APP/Contents/Info.plist"
plutil -lint "$APP/Contents/Info.plist" >/dev/null
printf 'APPL????' > "$APP/Contents/PkgInfo"
cp -R third_party/nebula third_party/rqlite third_party/go "$APP/Contents/Resources/licenses/"
printf 'Team: %s\nOptional free runner: %s\nDatabase: v%s (%s)\n' "$VERSION" "$RUNNER_VERSION" "$RQLITE_VERSION" "$RQLITE_COMMIT" > "$APP/Contents/Resources/components.txt"
sign "$H/werkbord-team"
sign "$H/werkbord"
sign "$APP"
MODE=--adhoc; [ "$IDENTITY" = - ] || MODE=--distribution
scripts/check-team-desktop.sh "$MODE" "$APP"
if [ -n "$RELEASE" ]; then scripts/notarize-desktop.sh --require app "$APP"; fi
[ "$("$H/werkbord-team" version)" = "$VERSION" ] || die "Team version does not match the window"
[ "$("$H/werkbord" version)" = "$RUNNER_VERSION" ] || die "the optional runner version is wrong"
mkdir -p "$OUT"
OUT=$(cd "$OUT" && pwd)
rm -rf "$OUT/Werkbord Team.app"
ditto "$APP" "$OUT/Werkbord Team.app"
if [ -n "$PACKAGE" ]; then
  DISK="$STAGE/disk"; mkdir -p "$DISK"
  ditto "$APP" "$DISK/Werkbord Team.app"
  ln -s /Applications "$DISK/Applications"
  DMG="WerkbordTeam_${VERSION#v}_darwin_${ARCH}.dmg"
  hdiutil create -quiet -volname "Werkbord Team" -srcfolder "$DISK" -fs HFS+ -format UDZO -ov "$OUT/$DMG"
  if [ "$IDENTITY" != - ]; then /usr/bin/codesign --force --sign "$IDENTITY" --timestamp "$OUT/$DMG"; fi
  if [ -n "$RELEASE" ]; then scripts/notarize-desktop.sh --require dmg "$OUT/$DMG"; fi
  (cd "$OUT" && shasum -a 256 "$DMG" > "$DMG.sha256")
fi
printf 'Built %s (Team %s).\n' "$OUT/Werkbord Team.app" "$VERSION"
if [ -z "$RELEASE" ]; then printf 'Development build. Distribution requires --release and the issuer/Apple signing credentials.\n'; fi
