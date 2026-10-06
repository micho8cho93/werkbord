#!/bin/sh
# Says whether the app's way of updating itself is what it must be: where it looks, who may sign what it installs,
# and that nothing in it asks the internet or installs anything on its own. One definition, used by the build,
# the tests, and the check of a published release.
#
#   scripts/check-desktop-updater.sh <Werkbord.app>              any build that has Sparkle
#   scripts/check-desktop-updater.sh --release <Werkbord.app>    what a published app must be
#
# Every build that has Sparkle must:
#   - carry the pinned Sparkle (desktop/build/sparkle.env) and no XPC services (the app is not sandboxed);
#   - hold a public EdDSA key (32 bytes), require a signed feed and verify an update's signature before opening it;
#   - have no schedule (SUEnableAutomaticChecks false) and send no profile of the computer (SUSendProfileInfo false);
#   - not weaken App Transport Security beyond local networking (the controller's own loopback address).
# A release must also:
#   - look for updates at exactly https://github.com/micho8cho93/werkbord/releases/latest/download/appcast.xml;
#   - hold exactly the public key committed in desktop/build/darwin/sparkle-public-key (SPARKLE_PUBLIC_KEY_FILE names
#     another file, for tests: nothing in a release sets it);
#   - install nothing unattended (SUAutomaticallyUpdate and SUAllowsAutomaticUpdates false);
#   - have no trace of the test hooks (an override of the feed, or the build that installs without asking).
# A release without Sparkle at all is refused: it is how a user's app gets its next version.
set -eu

die() { printf 'check-desktop-updater: %s\n' "$*" >&2; exit 1; }

release=""
app=""
for a in "$@"; do
  case "$a" in
    --release) release=1 ;;
    -*) die "unknown option $a" ;;
    *) app=$a ;;
  esac
done
[ -d "$app" ] || die "usage: check-desktop-updater.sh [--release] <Werkbord.app>"
root=$(cd "$(dirname "$0")/.." && pwd)
plist="$app/Contents/Info.plist"
PB=/usr/libexec/PlistBuddy
get() { $PB -c "Print :$1" "$plist" 2>/dev/null || true; }

feed=$(get SUFeedURL)
key=$(get SUPublicEDKey)
if [ -z "$feed" ] && [ -z "$key" ]; then
  [ -z "$release" ] || die "this app carries no updater: a release must (SUFeedURL and SUPublicEDKey are not in Info.plist)"
  echo "check-desktop-updater: this build has no updater (fine for a build for this computer)"
  exit 0
fi
[ -n "$feed" ] && [ -n "$key" ] || die "Info.plist has one of SUFeedURL and SUPublicEDKey but not both"

fw="$app/Contents/Frameworks/Sparkle.framework"
[ -d "$fw/Versions/B" ] || die "Info.plist names an update feed but the app has no Sparkle.framework"
[ ! -e "$fw/XPCServices" ] && [ ! -e "$fw/Versions/B/XPCServices" ] || die "Sparkle's XPC services are in the app: it is not sandboxed, and the installer must run from the helper Sparkle documents for that"
# shellcheck disable=SC1091
want=$(. "$root/desktop/build/sparkle.env" && printf '%s' "$SPARKLE_VERSION")
have=$($PB -c 'Print :CFBundleShortVersionString' "$fw/Versions/B/Resources/Info.plist")
[ "$have" = "$want" ] || die "the app carries Sparkle $have, but desktop/build/sparkle.env pins $want"

# the public key is an Ed25519 key: base64 of 32 bytes
n=$(printf '%s' "$key" | base64 --decode 2>/dev/null | wc -c | tr -d ' ')
[ "$n" = 32 ] || die "SUPublicEDKey is not an Ed25519 public key (base64 of 32 bytes; it decodes to $n)"

[ "$(get SURequireSignedFeed)" = true ] || die "SURequireSignedFeed is not true: a feed that is not signed would be believed"
[ "$(get SUVerifyUpdateBeforeExtraction)" = true ] || die "SUVerifyUpdateBeforeExtraction is not true: an archive must be verified before it is opened"
[ "$(get SUEnableAutomaticChecks)" = false ] || die "SUEnableAutomaticChecks is not false: the updater must have no schedule of its own"
[ "$(get SUSendProfileInfo)" = false ] || die "SUSendProfileInfo is not false: nothing about this computer is sent"
if $PB -c 'Print :NSAppTransportSecurity:NSAllowsArbitraryLoads' "$plist" >/dev/null 2>&1 || \
   $PB -c 'Print :NSAppTransportSecurity:NSAllowsArbitraryLoadsInWebContent' "$plist" >/dev/null 2>&1; then
  die "App Transport Security is switched off: the update feed and archives must be https"
fi

case "$feed" in
  https://*) ;;
  http://127.0.0.1:*|http://localhost:*) [ -z "$release" ] || die "the feed is $feed: a release uses the one https address" ;;
  *) die "the feed is $feed, which is not https" ;;
esac

if [ -n "$release" ]; then
  [ "$feed" = "https://github.com/micho8cho93/werkbord/releases/latest/download/appcast.xml" ] ||
    die "the feed is $feed; a release looks for updates at https://github.com/micho8cho93/werkbord/releases/latest/download/appcast.xml (the newest individual release's appcast, which a Team release can never be)"
  committed=$(tr -d '[:space:]' < "${SPARKLE_PUBLIC_KEY_FILE:-$root/desktop/build/darwin/sparkle-public-key}" 2>/dev/null || true)
  [ -n "$committed" ] && [ "$key" = "$committed" ] || die "the app's update key is not the one committed in desktop/build/darwin/sparkle-public-key: an update signed with the real key would be refused"
  [ "$(get SUAutomaticallyUpdate)" = false ] || die "SUAutomaticallyUpdate is not false: nothing may be installed unattended"
  [ "$(get SUAllowsAutomaticUpdates)" = false ] || die "SUAllowsAutomaticUpdates is not false"
  exe="$app/Contents/MacOS/Werkbord"
  if grep -aq 'WERKBORD_UPDATER_TEST' "$exe"; then die "the app contains the hook of the build that installs updates without asking"; fi
fi
echo "check-desktop-updater: ok: $app updates from $feed, signed with a key it holds, verified before it opens, on no schedule"
