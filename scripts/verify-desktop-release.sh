#!/bin/sh
# Checks a published desktop release as a person who downloads it would meet it. CI runs this on a fresh
# runner after the upload, and whoever releases runs it once by hand on the first real release:
#
#   scripts/verify-desktop-release.sh werkbord-v1.2.0
#
# It downloads the disk image and its checksum from the release page (no credentials, no GitHub
# API), and then, in order:
#   1. checks the disk image against its .sha256;
#   2. marks it as downloaded from the Internet (the quarantine attribute a browser sets, which is what
#      makes Gatekeeper look), and checks its signature, its stapled notarization ticket, and Gatekeeper's
#      verdict on the image;
#   3. mounts it and copies the app out, as dragging it to Applications does, marked the same way;
#   4. checks the app: every piece of code in it signed by the Developer ID with the hardened runtime
#      (scripts/check-desktop-signature.sh), the stapled ticket, and Gatekeeper's verdict
#      (it must say "Notarized Developer ID");
#   5. runs the program the app carries and checks it says the version of the tag, and that the app says
#      so too;
#   6. downloads the update archive the installed apps update from (Werkbord_<version>_darwin_<arch>.zip), checks it
#      against its .sha256, and checks it holds the very same app, signed and notarized as a release must be and with the
#      update settings a release must have (scripts/check-desktop-updater.sh), as releases from 1.2.0 on must;
#   7. checks the window and the program each hold Apple Silicon and Intel code, and that Werkbord.dmg, the download
#      link with no version in it (the website's button), is that same disk image.
# (The feed that points at that archive is checked by scripts/verify-appcast.sh.)
# Anything else is a failure with the step that failed. Nothing is installed, and what it mounts and
# copies is removed.
#
# Environment (for tests and mirrors): WERKBORD_RELEASE_BASE (default the GitHub releases' download
# address), DESKTOP_ARCH (default universal), and the programs it runs: CURL, XCRUN, SPCTL, CODESIGN, HDIUTIL.
set -eu

die() { printf 'verify-desktop-release: FAILED: %s\n' "$*" >&2; exit 1; }
step() { printf 'verify-desktop-release: %s\n' "$*"; }

tag=${1:-}
case "$tag" in
  werkbord-team-v*) die "$tag is a Werkbord Team release: the desktop app belongs to the individual product (werkbord-vX.Y.Z)" ;;
  werkbord-v[0-9]*.[0-9]*.[0-9]*) ;;
  *) die "usage: verify-desktop-release.sh werkbord-vX.Y.Z" ;;
esac
[ "$(uname -s)" = Darwin ] || die "a disk image is checked on macOS"
version=${tag#werkbord-v}
BASE=${WERKBORD_RELEASE_BASE:-https://github.com/micho8cho93/werkbord/releases/download}
ARCH=${DESKTOP_ARCH:-universal}
CURL=${CURL:-curl}
XCRUN=${XCRUN:-xcrun}
SPCTL=${SPCTL:-spctl}
CODESIGN=${CODESIGN:-codesign}
HDIUTIL=${HDIUTIL:-hdiutil}
root=$(cd "$(dirname "$0")/.." && pwd)

WORK=$(mktemp -d)
MNT=""
cleanup() {
  [ -z "$MNT" ] || $HDIUTIL detach "$MNT" -quiet -force >/dev/null 2>&1 || true
  rm -rf "$WORK"
}
trap cleanup EXIT INT TERM

# A release that was created a moment ago can take a little while to be served; a file:// base (a test) is not waited for.
case "$BASE" in file://*) RETRY="" ;; *) RETRY="--retry 6 --retry-delay 10 --retry-all-errors" ;; esac
dmg="Werkbord_${version}_darwin_${ARCH}.dmg"
step "downloading $dmg from $BASE/$tag"
$CURL -fsSL $RETRY -o "$WORK/$dmg" "$BASE/$tag/$dmg" || die "could not download $BASE/$tag/$dmg: the release has no disk image"
$CURL -fsSL $RETRY -o "$WORK/$dmg.sha256" "$BASE/$tag/$dmg.sha256" || die "could not download $dmg.sha256: the release has no checksum for the disk image"

# 1. The checksum the release publishes.
want=$(awk '{print $1}' "$WORK/$dmg.sha256")
have=$(shasum -a 256 "$WORK/$dmg" | awk '{print $1}')
[ -n "$want" ] && [ "$want" = "$have" ] || die "the disk image's sha256 is $have, the release says ${want:-nothing}"
step "ok  sha256 matches ($have)"

# 2. As a browser leaves it: quarantined.
xattr -w com.apple.quarantine "0083;$(printf '%x' "$(date +%s)");Safari;" "$WORK/$dmg"
"$CODESIGN" --verify --strict "$WORK/$dmg" 2>"$WORK/err" || { cat "$WORK/err" >&2; die "the disk image's signature does not verify"; }
CODESIGN="$CODESIGN" "$root/scripts/check-desktop-signature.sh" --distribution "$WORK/$dmg" >/dev/null || die "the disk image is not signed with a Developer ID Application certificate"
$XCRUN stapler validate "$WORK/$dmg" >"$WORK/out" 2>&1 || { cat "$WORK/out" >&2; die "the disk image has no valid notarization ticket stapled to it"; }
step "ok  the disk image is signed by the Developer ID, and its notarization ticket is stapled"
$SPCTL --assess --type open --context context:primary-signature --verbose=2 "$WORK/$dmg" >"$WORK/out" 2>&1 || { cat "$WORK/out" >&2; die "Gatekeeper rejects the disk image"; }
step "ok  Gatekeeper accepts the disk image ($(tr '\n' ' ' < "$WORK/out" | sed 's/[[:space:]]*$//'))"

# 3. Open it and drag the app out.
MNT="$WORK/mnt"
mkdir -p "$MNT"
$HDIUTIL attach "$WORK/$dmg" -nobrowse -readonly -noautoopen -mountpoint "$MNT" -quiet || die "the disk image does not mount"
[ -d "$MNT/Werkbord.app" ] || die "the disk image has no Werkbord.app"
[ -L "$MNT/Applications" ] || die "the disk image has no shortcut to Applications to drag the app onto"
mkdir -p "$WORK/Applications"
ditto "$MNT/Werkbord.app" "$WORK/Applications/Werkbord.app"
APP="$WORK/Applications/Werkbord.app"
$HDIUTIL detach "$MNT" -quiet >/dev/null 2>&1 || $HDIUTIL detach "$MNT" -quiet -force >/dev/null 2>&1 || true
MNT=""

# What the app and the program say about themselves is read first, on a copy that is not marked as downloaded: files from a
# mounted image carry the mark, macOS will not run marked code that Gatekeeper has not accepted, and whether it accepts is the
# next check's business, which has a clearer message than "killed".
xattr -dr com.apple.quarantine "$APP" 2>/dev/null || true
reported=$("$APP/Contents/Helpers/werkbord" version) || die "the program inside the app does not run"
[ "$reported" = "v$version" ] || die "the program inside the app says $reported, the release is v$version"
plist=$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$APP/Contents/Info.plist")
[ "$plist" = "$version" ] || die "the app says it is version $plist, the release is $version"
step "ok  the app and the program it carries are both $version"
xattr -w com.apple.quarantine "0083;$(printf '%x' "$(date +%s)");Safari;" "$APP"

# Unified releases carry the Team service and its installer in the app itself, signed by the same team.
case "$version" in
  [0-3].*) ;;
  *)
    [ -f "$APP/Contents/Helpers/werkbord-team" ] || die "the app carries no Team service"
    "$root/scripts/check-team-payload.sh" --distribution "$APP" || die "Team's service in the app is not what a release must carry"
    [ "$("$APP/Contents/Helpers/werkbord-team" version)" = "v$version" ] || die "the Team service in the app is not this release (v$version)"
    "$APP/Contents/MacOS/Werkbord" --verify-release || die "the app is not signed by the release team, which its Team installer requires"
    step "ok  the Team service is this release, and the app is signed by the team its installer requires"
    ;;
esac

# 4. The app, on its own.
"$CODESIGN" --verify --strict --deep --verbose=2 "$APP" 2>"$WORK/err" || { cat "$WORK/err" >&2; die "the app's signature does not verify (codesign --verify --deep --strict)"; }
CODESIGN="$CODESIGN" "$root/scripts/check-desktop-signature.sh" --distribution "$APP" >"$WORK/out" 2>&1 || { cat "$WORK/out" >&2; die "the app is not signed the way a release must be"; }
step "ok  codesign --verify --deep --strict passes; every piece of code is Developer ID signed with the hardened runtime"
$XCRUN stapler validate "$APP" >"$WORK/out" 2>&1 || { cat "$WORK/out" >&2; die "the app has no valid notarization ticket stapled to it (it would need the network to open)"; }
step "ok  the app's notarization ticket is stapled"
$SPCTL --assess --type execute --verbose=2 "$APP" >"$WORK/out" 2>&1 || { cat "$WORK/out" >&2; die "Gatekeeper rejects the app, as it would for a person who downloaded it"; }
case $(cat "$WORK/out") in
  *"Notarized Developer ID"*) ;;
  *) cat "$WORK/out" >&2; die "Gatekeeper accepts the app but not as a Notarized Developer ID" ;;
esac
step "ok  Gatekeeper accepts the app: Notarized Developer ID"

# 5. The archive the app updates itself from: the very same app, and nothing else. (Releases before 1.2.0 had none.)
case "$version" in
  0.*|1.0.*|1.1.*) step "ok  (version $version is from before the app updated itself: it has no update archive)" ;;
  *)
    zip="Werkbord_${version}_darwin_${ARCH}.zip"
    # shellcheck disable=SC2086
    $CURL -fsSL $RETRY -o "$WORK/$zip" "$BASE/$tag/$zip" || die "the release has no update archive $zip: installed apps could not be updated"
    # shellcheck disable=SC2086
    $CURL -fsSL $RETRY -o "$WORK/$zip.sha256" "$BASE/$tag/$zip.sha256" || die "the release has no checksum for $zip"
    [ "$(awk '{print $1}' "$WORK/$zip.sha256")" = "$(shasum -a 256 "$WORK/$zip" | awk '{print $1}')" ] || die "the update archive does not match its checksum"
    mkdir -p "$WORK/unzipped"
    ditto -x -k "$WORK/$zip" "$WORK/unzipped"
    UAPP="$WORK/unzipped/Werkbord.app"
    [ -d "$UAPP" ] || die "the update archive holds no Werkbord.app"
    "$CODESIGN" --verify --strict --deep "$UAPP" 2>"$WORK/err" || { cat "$WORK/err" >&2; die "the app in the update archive does not verify"; }
    CODESIGN="$CODESIGN" "$root/scripts/check-desktop-signature.sh" --distribution "$UAPP" >"$WORK/out" 2>&1 || { cat "$WORK/out" >&2; die "the app in the update archive is not signed the way a release must be"; }
    "$root/scripts/check-desktop-updater.sh" --release "$UAPP" >"$WORK/out" 2>&1 || { cat "$WORK/out" >&2; die "the app in the update archive has the wrong update settings"; }
    $XCRUN stapler validate "$UAPP" >"$WORK/out" 2>&1 || { cat "$WORK/out" >&2; die "the app in the update archive has no notarization ticket stapled"; }
    same_a=$("$CODESIGN" -dvvv "$APP" 2>&1 | sed -n 's/^CDHash=//p' | head -1)
    same_b=$("$CODESIGN" -dvvv "$UAPP" 2>&1 | sed -n 's/^CDHash=//p' | head -1)
    [ -n "$same_a" ] && [ "$same_a" = "$same_b" ] || die "the app in the update archive is not the app in the disk image"
    step "ok  the update archive holds the same notarized app as the disk image, with the update settings a release must have"
    ;;
esac

# 6b. The link with no version in it, which the website and a one-line curl use, is that same disk image.
case "$version" in
  0.*|1.0.*|1.1.*) ;;
  *)
    # shellcheck disable=SC2086
    $CURL -fsSL $RETRY -o "$WORK/Werkbord.dmg" "$BASE/$tag/Werkbord.dmg" || die "the release has no Werkbord.dmg, the download link that never changes (the website's button)"
    [ "$(shasum -a 256 "$WORK/Werkbord.dmg" | awk '{print $1}')" = "$have" ] || die "Werkbord.dmg is not the disk image of $tag"
    step "ok  Werkbord.dmg, the link with no version in it, is this disk image"
    ;;
esac

# 7. Both kinds of Mac.
if [ "$ARCH" = universal ] && command -v lipo >/dev/null 2>&1; then
  for exe in "$APP/Contents/MacOS/Werkbord" "$APP/Contents/Helpers/werkbord"; do
    archs=$(lipo -archs "$exe")
    case " $archs " in *" arm64 "*) ;; *) die "$exe has no Apple Silicon code ($archs)" ;; esac
    case " $archs " in *" x86_64 "*) ;; *) die "$exe has no Intel code ($archs)" ;; esac
  done
  step "ok  the window and the program run on Apple Silicon and Intel"
fi

step "$tag is a good release"
