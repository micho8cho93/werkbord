#!/bin/sh
# Tests scripts/check-desktop-updater.sh: that it accepts an app with the update settings a release must have, and rejects each
# single way of not having them. It needs no build and no signing (the settings are in Info.plist and a framework's folder),
# only macOS's PlistBuddy, and runs in a second.
#
#   scripts/test-desktop-updater-config.sh
set -eu

cd "$(dirname "$0")/.."
[ "$(uname -s)" = Darwin ] || { echo "test-desktop-updater-config: needs macOS"; exit 0; }
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT INT TERM
PB=/usr/libexec/PlistBuddy
pass=0
ok() { pass=$((pass + 1)); printf '  ok  %s\n' "$1"; }
bad() { printf 'FAIL  %s\n' "$1" >&2; [ -n "${2:-}" ] && printf '%s\n' "$2" >&2; exit 1; }
contains() { case "$1" in *"$2"*) return 0 ;; esac; return 1; }

sparkle_version=$(. desktop/build/sparkle.env && printf '%s' "$SPARKLE_VERSION")
KEY=$(head -c 32 /dev/urandom | base64)
printf '%s' "$KEY" > "$WORK/committed-key"
export SPARKLE_PUBLIC_KEY_FILE="$WORK/committed-key"
FEED=https://github.com/micho8cho93/werkbord/releases/latest/download/appcast.xml

# app <name>: a bundle that has everything a release's does (its code need not be real: only the settings are read)
app() {
  a="$WORK/$1/Werkbord.app"; mkdir -p "$a/Contents/MacOS" "$a/Contents/Frameworks/Sparkle.framework/Versions/B/Resources"
  printf 'int main(void){return 0;}\n' > "$WORK/$1.c"; clang "$WORK/$1.c" -o "$a/Contents/MacOS/Werkbord"
  cp desktop/build/darwin/Info.plist "$a/Contents/Info.plist"; sed -i '' 's/@VERSION@/1.2.3/' "$a/Contents/Info.plist"
  P="$a/Contents/Info.plist"
  $PB -c "Add :SUFeedURL string $FEED" "$P"
  $PB -c "Add :SUPublicEDKey string $KEY" "$P"
  for k in SUEnableAutomaticChecks SUSendProfileInfo SUAllowsAutomaticUpdates SUAutomaticallyUpdate; do $PB -c "Add :$k bool false" "$P"; done
  for k in SUVerifyUpdateBeforeExtraction SURequireSignedFeed; do $PB -c "Add :$k bool true" "$P"; done
  printf '<?xml version="1.0" encoding="UTF-8"?>\n<plist version="1.0"><dict><key>CFBundleShortVersionString</key><string>%s</string></dict></plist>\n' "$sparkle_version" > "$a/Contents/Frameworks/Sparkle.framework/Versions/B/Resources/Info.plist"
}

app good
out=$(scripts/check-desktop-updater.sh --release "$WORK/good/Werkbord.app" 2>&1) || bad "an app with a release's update settings was rejected" "$out"
ok "an app with the update settings a release must have passes --release"

# one thing wrong at a time
wrong() { # <name> <what is wrong> <expected message> <how to break it: a shell command on $a / $P>
  app "$1"; a="$WORK/$1/Werkbord.app"; P="$a/Contents/Info.plist"
  eval "$4"
  out=$(scripts/check-desktop-updater.sh --release "$a" 2>&1) && bad "$1: an app $2 was accepted" "$out"
  contains "$out" "$3" || bad "$1: the message does not say \"$3\"" "$out"
  ok "--release rejects an app $2"
}
wrong feedelsewhere "that looks for updates somewhere else" "a release looks for updates at" '$PB -c "Set :SUFeedURL https://example.com/appcast.xml" "$P"'
wrong feedhttp "whose feed is plain http" "not https" '$PB -c "Set :SUFeedURL http://example.com/appcast.xml" "$P"'
wrong feedlocal "whose feed is a local server" "a release uses the one https address" '$PB -c "Set :SUFeedURL http://127.0.0.1:8000/appcast.xml" "$P"'
wrong otherkey "that holds another public key than the committed one" "not the one committed" '$PB -c "Set :SUPublicEDKey $(head -c 32 /dev/urandom | base64)" "$P"'
wrong shortkey "whose update key is not an Ed25519 key" "Ed25519" '$PB -c "Set :SUPublicEDKey c2hvcnQ=" "$P"'
wrong nokey "that has a feed but no key" "one of SUFeedURL and SUPublicEDKey" '$PB -c "Delete :SUPublicEDKey" "$P"'
wrong unsignedfeedok "that would believe an unsigned feed" "SURequireSignedFeed" '$PB -c "Set :SURequireSignedFeed false" "$P"'
wrong noverify "that opens an archive before verifying it" "SUVerifyUpdateBeforeExtraction" '$PB -c "Set :SUVerifyUpdateBeforeExtraction false" "$P"'
wrong schedule "that checks on a schedule of its own" "no schedule" '$PB -c "Set :SUEnableAutomaticChecks true" "$P"'
wrong profile "that sends a profile of the computer" "SUSendProfileInfo" '$PB -c "Set :SUSendProfileInfo true" "$P"'
wrong unattended "that installs updates unattended" "unattended" '$PB -c "Set :SUAutomaticallyUpdate true" "$P"'
wrong allowsauto "that offers automatic updates" "SUAllowsAutomaticUpdates" '$PB -c "Set :SUAllowsAutomaticUpdates true" "$P"'
wrong ats "with App Transport Security off" "App Transport Security" '$PB -c "Add :NSAppTransportSecurity:NSAllowsArbitraryLoads bool true" "$P"'
wrong xpc "that carries Sparkle's XPC services" "XPC services" 'mkdir -p "$a/Contents/Frameworks/Sparkle.framework/Versions/B/XPCServices"'
wrong oldsparkle "that carries another Sparkle than the one pinned" "pins $sparkle_version" '$PB -c "Set :CFBundleShortVersionString 2.0.0" "$a/Contents/Frameworks/Sparkle.framework/Versions/B/Resources/Info.plist"'
wrong noframework "that names a feed but has no Sparkle" "no Sparkle.framework" 'rm -r "$a/Contents/Frameworks/Sparkle.framework"'
printf 'x WERKBORD_UPDATER_TEST x' >> "$WORK/good/Werkbord.app/Contents/MacOS/Werkbord"
out=$(scripts/check-desktop-updater.sh --release "$WORK/good/Werkbord.app" 2>&1) && bad "an app with the test hook in it was accepted" "$out"
contains "$out" "hook of the build that installs updates without asking" || bad "the test hook's message" "$out"
ok "--release rejects an app that contains the test build's hook"

# no updater at all: fine for a build for this computer, never for a release
app none; a="$WORK/none/Werkbord.app"; P="$a/Contents/Info.plist"
for k in SUFeedURL SUPublicEDKey; do $PB -c "Delete :$k" "$P"; done
scripts/check-desktop-updater.sh "$a" >/dev/null || bad "a build with no updater must be fine for this computer"
out=$(scripts/check-desktop-updater.sh --release "$a" 2>&1) && bad "a release with no updater was accepted" "$out"
ok "no updater is fine for a build for this computer, and never for a release"

# a test build (a local feed, the unattended settings) is allowed outside a release
app testbuild; a="$WORK/testbuild/Werkbord.app"; P="$a/Contents/Info.plist"
$PB -c "Set :SUFeedURL http://127.0.0.1:18080/appcast.xml" "$P"; $PB -c "Set :SUAutomaticallyUpdate true" "$P"
scripts/check-desktop-updater.sh "$a" >/dev/null || bad "a test build's settings must pass the check that is not --release"
ok "a test build's local feed passes the check that is not --release"

echo "ok: $pass checks"
