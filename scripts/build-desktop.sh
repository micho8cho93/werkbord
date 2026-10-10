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
#   SPARKLE_PUBLIC_KEY the public half of the update-signing key (base64). Default: the one in
#                      desktop/build/darwin/sparkle-public-key, which is committed (docs/DESKTOP_RELEASE.md, step 6).
#                      With a key, the app carries Sparkle and can replace itself; without one it does not, and updates
#                      its program only, as before. A release must have one. SPARKLE=0 leaves Sparkle out.
#
# With --package and Sparkle, the disk image is joined by Werkbord_<version>_darwin_<arch>.zip, the archive Sparkle
# installs from (scripts/make-appcast.sh signs it and writes the feed).
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
#   SPARKLE_FEED_URL=…        where the app looks for updates (a local server, for scripts/test-desktop-update.sh); a release
#                             always has the one address below, and is checked for it
#   UPDATER_TEST=1            the build that installs a valid update without asking, so the real updater can be run unattended
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
[ -f desktop/frontend/dist/shell/shell/index.html ] || die "the workspace shell is not built: run make web-shell first"
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
# Where the app looks for updates: the appcast every individual release uploads. /releases/latest/download/<file> follows the
# release marked "latest", which only individual releases are (release.yml), so a Team release can never be the feed.
FEED_URL=https://github.com/micho8cho93/werkbord/releases/latest/download/appcast.xml
SPARKLE_KEY=${SPARKLE_PUBLIC_KEY:-}
[ -n "$SPARKLE_KEY" ] || { [ -f desktop/build/darwin/sparkle-public-key ] && SPARKLE_KEY=$(tr -d '[:space:]' < desktop/build/darwin/sparkle-public-key); } || true
[ "${SPARKLE:-}" != 0 ] || SPARKLE_KEY=""
WITH_SPARKLE=""
[ -z "$SPARKLE_KEY" ] || WITH_SPARKLE=1
GO_TAGS=desktop,production
[ -z "${UPDATER_TEST:-}" ] || GO_TAGS=$GO_TAGS,updatertest

ENTITLEMENTS=desktop/build/darwin/entitlements.plist

# ---- what a release build insists on, before anything is built ----
if [ -n "$RELEASE" ]; then
  [ "$IDENTITY" != "-" ] && [ -n "$IDENTITY" ] ||
    die "a release build must be signed with a Developer ID Application identity, never ad hoc: set CODESIGN_IDENTITY (docs/DESKTOP_RELEASE.md, steps 2-3 and 5)"
  [ -z "$TIMESTAMP" ] || die "CODESIGN_TIMESTAMP is for tests only: a release is signed with Apple's secure timestamp"
  [ "$CODESIGN" = codesign ] || die "CODESIGN is for tests only: a release is signed with the system's codesign"
  [ "$HDIUTIL" = hdiutil ] || die "HDIUTIL is for tests only"
  [ -z "${SPARKLE_FEED_URL:-}" ] || die "SPARKLE_FEED_URL is for tests only: a release looks for updates at $FEED_URL"
  [ -z "${UPDATER_TEST:-}" ] || die "UPDATER_TEST is for tests only: that build installs updates without asking"
  [ -z "${SPARKLE_PUBLIC_KEY:-}" ] || die "SPARKLE_PUBLIC_KEY is for tests only: a release carries the key committed in desktop/build/darwin/sparkle-public-key"
  [ "${SPARKLE:-}" != 0 ] || die "a release carries Sparkle: the app must be able to update itself"
  [ -z "${XCRUN:-}${SPCTL:-}${DITTO:-}" ] || die "XCRUN, SPCTL and DITTO are for tests only: a release is notarized with Apple's own tools"
  [ "$ARCH" = universal ] || die "a release is one universal disk image (ARCH=universal), so that nobody has to know their chip; not \"$ARCH\""
  printf '%s' "$VERSION" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$' ||
    die "a release is built from a version that is exactly a release (v1.2.3), not \"$VERSION\""
  [ "$VERSION" = "v$(scripts/product.sh werkbord version)" ] ||
    die "VERSION $VERSION is not what cmd/werkbord/VERSION says (v$(scripts/product.sh werkbord version))"
  # A release is notarized, so it must be able to be: found out now and not after the build.
  [ -n "${NOTARY_KEY_FILE:-}" ] && [ -n "${NOTARY_KEY_ID:-}" ] && [ -n "${NOTARY_ISSUER:-}" ] ||
    die "a release is notarized: NOTARY_KEY_FILE, NOTARY_KEY_ID and NOTARY_ISSUER must all be set (docs/DESKTOP_RELEASE.md, steps 4-5)"
  # ... and it carries the updater, which needs the public half of the update-signing key to be committed.
  [ -n "$SPARKLE_KEY" ] || die "a release needs the update-signing public key in desktop/build/darwin/sparkle-public-key (docs/DESKTOP_RELEASE.md, step 6)"
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
      go build -trimpath -tags "$GO_TAGS" -ldflags "-s -w -X main.version=$VERSION" \
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

# The app carries Team's native installer, the nested Werkbord Team.app. It is part of this release: built here, from this
# commit, at this version, and signed with the same Developer ID. It is inert until a person asks to add a Team.
# A release signs it as a release (and Apple notarizes it); its service installer later refuses to run unless the app was
# signed by this Apple Developer team (cmd/werkbord-team/desktop/internal/platform/release.go).
TEAM_APP=${TEAM_DESKTOP_APP:-}
if [ "${UNIFIED_DESKTOP:-1}" = 0 ]; then
  [ -z "$RELEASE" ] || die "a release carries Team's installer"
else
  [ -z "$TEAM_APP" ] || [ -z "$RELEASE" ] || die "TEAM_DESKTOP_APP is for tests only: a release builds Team's installer itself, from this commit"
  if [ -z "$TEAM_APP" ]; then
    if [ -n "$RELEASE" ]; then
      ARCH="$ARCH" VERSION="$VERSION" CODESIGN_IDENTITY="$IDENTITY" OUT="$STAGE/team" scripts/build-team-desktop.sh --nested
    else
      ARCH="$ARCH" VERSION="$VERSION" OUT="$STAGE/team" scripts/build-team-desktop.sh
    fi
    TEAM_APP="$STAGE/team/Werkbord Team.app"
  fi
  if [ -n "$RELEASE" ]; then
    scripts/check-team-desktop.sh --distribution "$TEAM_APP"
    "$TEAM_APP/Contents/MacOS/Werkbord Team" --verify-release
    xcrun stapler validate "$TEAM_APP"
    spctl --assess --type execute --verbose=2 "$TEAM_APP"
  else
    scripts/check-team-desktop.sh --adhoc "$TEAM_APP"
  fi
  ditto "$TEAM_APP" "$APP/Contents/Helpers/Werkbord Team.app"
fi
TEAM_VERSION=absent
if [ -d "$APP/Contents/Helpers/Werkbord Team.app" ]; then
  TEAM_VERSION=$("$APP/Contents/Helpers/Werkbord Team.app/Contents/Helpers/werkbord-team" version)
fi
case "$TEAM_VERSION" in "$VERSION"|absent) ;; *) die "Team $TEAM_VERSION is not this release ($VERSION)" ;; esac
printf 'Shell: %s\nPersonal: %s\nTeam: %s\nPersonal API: workspace-summary-v1 + execution-local-v1\nTeam API: device-v1 + team-v1\nSync: integration-v1 + execution-v1\n' "$VERSION" "$VERSION" "$TEAM_VERSION" > "$APP/Contents/Resources/components.txt"

cp desktop/build/compatibility.json "$APP/Contents/Resources/compatibility.json"
if [ "${UNIFIED_DESKTOP:-1}" != 0 ]; then scripts/check-unified-desktop.sh --development "$APP"; fi

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

# 4b. Sparkle, which replaces the app in place. Loaded at run time from Contents/Frameworks (desktop/updater_darwin.m), so
# the window works whether or not it is there. Its keys say where the feed is and who may sign what it offers; every key
# that would make it ask the internet on its own, or install something unattended, is off.
if [ -n "$WITH_SPARKLE" ]; then
  SPARKLE_HOME=$(scripts/fetch-sparkle.sh) || die "Sparkle is not available (scripts/fetch-sparkle.sh)"
  mkdir -p "$APP/Contents/Frameworks"
  ditto "$SPARKLE_HOME/Sparkle.framework" "$APP/Contents/Frameworks/Sparkle.framework"
  # The XPC services are for sandboxed apps (an app that cannot start a process or write where it wants). This one is not
  # sandboxed (it runs the person's agents), so Sparkle's installer runs from its own helper, as Sparkle's documentation says.
  rm -rf "$APP/Contents/Frameworks/Sparkle.framework/Versions/B/XPCServices" "$APP/Contents/Frameworks/Sparkle.framework/XPCServices"
  PB=/usr/libexec/PlistBuddy
  P="$APP/Contents/Info.plist"
  $PB -c "Add :SUFeedURL string ${SPARKLE_FEED_URL:-$FEED_URL}" "$P"
  $PB -c "Add :SUPublicEDKey string $SPARKLE_KEY" "$P"
  $PB -c "Add :SUEnableAutomaticChecks bool false" "$P"      # no schedule: it asks only when a person does
  $PB -c "Add :SUSendProfileInfo bool false" "$P"            # nothing about this computer is sent with the request
  $PB -c "Add :SUVerifyUpdateBeforeExtraction bool true" "$P" # the EdDSA signature is checked before the archive is opened
  $PB -c "Add :SURequireSignedFeed bool true" "$P"           # the feed itself must be signed by the same key
  if [ -n "${UPDATER_TEST:-}" ]; then
    # A test app has its own identity (its preferences, caches and single-instance lock are not the real app's) and takes its
    # environment from a file, because the system relaunches it after an update with none (desktop/updater_testenv_darwin.go).
    [ -n "${UPDATER_TEST_ENV_FILE:-}" ] || die "UPDATER_TEST needs UPDATER_TEST_ENV_FILE, the file the test app takes its environment from"
    $PB -c "Set :CFBundleIdentifier dev.werkbord.desktop.test" "$P"
    $PB -c "Add :WBTestEnvFile string $UPDATER_TEST_ENV_FILE" "$P"
    $PB -c "Add :SUAllowsAutomaticUpdates bool true" "$P"
    $PB -c "Add :SUAutomaticallyUpdate bool true" "$P"
    echo "build-desktop: TEST ONLY: this app installs a valid update without asking. Do not distribute this build." >&2
  else
    $PB -c "Add :SUAllowsAutomaticUpdates bool false" "$P"
    $PB -c "Add :SUAutomaticallyUpdate bool false" "$P"
  fi
  plutil -lint "$P" >/dev/null
fi

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
  if [ -n "$WITH_SPARKLE" ]; then
    # Sparkle's own, innermost first, as its documentation has it: the installer and the updater app it starts, then
    # the framework that holds them. All with this identity, so that the framework loads under the hardened runtime
    # (library validation wants the same team) and Sparkle accepts an update signed by the same one.
    fw="$app/Contents/Frameworks/Sparkle.framework"
    sign_code "$fw/Versions/B/Autoupdate"
    sign_code "$fw/Versions/B/Updater.app"
    sign_code "$fw"
  fi
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

# 5a. What the app says about updating is what it should: where it looks, who may sign what it installs, nothing unattended.
if [ -n "$WITH_SPARKLE" ] && [ -n "$RELEASE" ]; then
  scripts/check-desktop-updater.sh --release "$APP" || die "the app's update settings are not what a release must have"
elif [ -n "$WITH_SPARKLE" ]; then
  scripts/check-desktop-updater.sh "$APP" || die "the app's update settings are not what they should be"
fi

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
  if [ -n "$WITH_SPARKLE" ]; then
    # What Sparkle installs from: the app as it is now (signed, notarized, stapled), zipped the way Finder does.
    ZIP="Werkbord_${VERSION#v}_darwin_${ARCH}.zip"
    rm -f "$OUT/$ZIP" "$OUT/$ZIP.sha256"
    ditto -c -k --sequesterRsrc --keepParent "$APP" "$OUT/$ZIP"
    (cd "$OUT" && shasum -a 256 "$ZIP" > "$ZIP.sha256")
    echo "wrote $OUT/$ZIP ($(du -h "$OUT/$ZIP" | cut -f1)), for scripts/make-appcast.sh"
  fi
  echo "wrote $OUT/$DMG ($(du -h "$OUT/$DMG" | cut -f1))"
  if [ "$IDENTITY" = "-" ]; then
    echo "note: signed ad hoc. Someone else's Mac will warn that it cannot check the app until it is signed with a Developer ID and notarized (docs/DESKTOP.md)."
  elif [ -z "$NOTARIZE" ]; then
    echo "note: signed, not notarized. Someone else's Mac will warn that it cannot check the app (--notarize, or --release)."
  fi
fi
