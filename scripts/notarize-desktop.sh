#!/bin/sh
# Notarizes a signed Werkbord.app or disk image with Apple, staples the result to it, and checks that
# Gatekeeper accepts it.
#
#   scripts/notarize-desktop.sh [--require] app <Werkbord.app>
#   scripts/notarize-desktop.sh [--require] dmg <Werkbord_….dmg>
#
# Credentials are the App Store Connect API key (not an Apple ID password), from the environment and
# nowhere else; nothing here prints them, and the command lines that carry them are never echoed:
#   NOTARY_KEY_FILE   the path to the private key file (AuthKey_XXXXXXXXXX.p8)
#   NOTARY_KEY_ID     its Key ID
#   NOTARY_ISSUER     the Issuer ID (a UUID, shown at the top of the Keys page)
# With any of them missing it says which, on one line, and exits cleanly having done nothing; with --require
# (which a release uses) that is a failure, so a release cannot go out un-notarized.
#
# What it does, in this order, for either kind:
#   1. submits to Apple's notary service and waits (`notarytool submit --wait`). It does not trust the
#      program's exit status for the verdict: it reads the status Apple returned, and only "Accepted" is a
#      pass. Anything else prints Apple's own log for the submission (which says which file was wrong and
#      why) and fails. A submission that never started (a network error) is retried.
#   2. staples the ticket to the file (`stapler staple`), so that it opens with no network, and checks it
#      (`stapler validate`).
#   3. asks Gatekeeper what a user's Mac will say: `spctl --assess --type execute` for the app, and
#      `--type open --context context:primary-signature` for the disk image.
#
# Why the app is notarized and stapled BEFORE it goes into the disk image, and then the image again
# (scripts/build-desktop.sh does it in that order): a ticket stapled to the image is not carried to the app
# when someone drags the app out of it, so an app that was only notarized as part of the image has to ask Apple
# on its first launch, and cannot open offline. Stapling the app first costs one extra submission and
# gives an app that is complete on its own wherever it is copied to.
#
# Everything it runs can be replaced for a test: XCRUN, SPCTL, DITTO (and NOTARY_RETRY_SLEEP).
set -eu

XCRUN=${XCRUN:-xcrun}
SPCTL=${SPCTL:-spctl}
DITTO=${DITTO:-ditto}
SLEEP=${NOTARY_RETRY_SLEEP:-20}

say() { printf 'notarize-desktop: %s\n' "$*"; }
die() { printf 'notarize-desktop: %s\n' "$*" >&2; exit 1; }

require=""
if [ "${1:-}" = --require ]; then require=1; shift; fi
kind=${1:-}
target=${2:-}
case "$kind" in app|dmg) ;; *) die "usage: notarize-desktop.sh [--require] app <Werkbord.app> | dmg <disk image>" ;; esac
[ -n "$target" ] && [ -e "$target" ] || die "there is nothing at \"$target\" to notarize"

missing=""
[ -n "${NOTARY_KEY_FILE:-}" ] || missing="NOTARY_KEY_FILE"
[ -n "${NOTARY_KEY_ID:-}" ] || missing="${missing:+$missing, }NOTARY_KEY_ID"
[ -n "${NOTARY_ISSUER:-}" ] || missing="${missing:+$missing, }NOTARY_ISSUER"
if [ -n "$missing" ]; then
  msg="no notarization credentials: $missing not set (docs/DESKTOP_RELEASE.md, step 5)"
  if [ -n "$require" ]; then die "$msg"; fi
  say "$msg; not notarizing"
  exit 0
fi
[ -r "$NOTARY_KEY_FILE" ] || die "NOTARY_KEY_FILE does not name a readable file"

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT INT TERM

# Whatever Apple's tools say is shown with the credentials removed, in case one of them is echoed back.
scrub() { sed -e "s|$NOTARY_KEY_FILE|<key file>|g" -e "s|$NOTARY_KEY_ID|<key id>|g" -e "s|$NOTARY_ISSUER|<issuer>|g"; }
notary() { # <notarytool subcommand and arguments…>: the credentials added
  sub=$1; shift
  "$XCRUN" notarytool "$sub" "$@" --key "$NOTARY_KEY_FILE" --key-id "$NOTARY_KEY_ID" --issuer "$NOTARY_ISSUER"
}
# json_field <name> <file>: a string field of notarytool's JSON, which is flat and simple.
json_field() { tr -d '\n' < "$2" | sed -n "s/.*\"$1\"[[:space:]]*:[[:space:]]*\"\([^\"]*\)\".*/\1/p"; }

# 1. What is sent: a disk image as it is, an app as a zip of it (ditto keeps what a zip tool loses).
case "$kind" in
app)
  upload="$WORK/Werkbord.zip"
  "$DITTO" -c -k --keepParent "$target" "$upload" || die "could not zip $target to send it to Apple"
  ;;
dmg) upload=$target ;;
esac

say "submitting $(basename "$target") to Apple's notary service (this takes a few minutes)"
id=""; status=""; attempt=0
while :; do
  attempt=$((attempt + 1))
  : > "$WORK/submit.json"
  notary submit "$upload" --wait --timeout "${NOTARY_TIMEOUT:-45m}" --output-format json > "$WORK/submit.json" 2> "$WORK/submit.err" || true
  id=$(json_field id "$WORK/submit.json")
  status=$(json_field status "$WORK/submit.json")
  [ -z "$id" ] || break
  # Nothing was submitted (the network, or the credentials): say what Apple's tool said, and try again if it was not the credentials.
  scrub < "$WORK/submit.err" >&2
  scrub < "$WORK/submit.json" >&2
  if grep -qiE 'unauthorized|401|invalid credentials|forbidden|403' "$WORK/submit.err" "$WORK/submit.json" 2>/dev/null; then
    die "Apple refused the credentials: check NOTARY_KEY_FILE, NOTARY_KEY_ID and NOTARY_ISSUER (docs/DESKTOP_RELEASE.md, step 5)"
  fi
  [ $attempt -lt 3 ] || die "could not submit $(basename "$target") to Apple after $attempt attempts"
  say "the submission did not start; trying again"
  sleep "$SLEEP"
done

if [ "$status" != Accepted ]; then
  say "Apple's verdict for submission $id: ${status:-no status}"
  notary log "$id" > "$WORK/log.json" 2> "$WORK/log.err" || true
  if [ -s "$WORK/log.json" ]; then scrub < "$WORK/log.json" >&2; else scrub < "$WORK/log.err" >&2; fi
  die "notarization of $(basename "$target") was not accepted (see the log above, which names the files Apple objected to)"
fi
say "accepted by Apple (submission $id)"

# 2. Staple, and check that the ticket is really in the file.
"$XCRUN" stapler staple "$target" > "$WORK/staple.out" 2>&1 || { scrub < "$WORK/staple.out" >&2; die "could not staple the ticket to $(basename "$target")"; }
"$XCRUN" stapler validate "$target" > "$WORK/validate.out" 2>&1 || { scrub < "$WORK/validate.out" >&2; die "the ticket stapled to $(basename "$target") does not validate"; }
say "stapled and validated"

# 3. What a user's Mac says.
case "$kind" in
app) "$SPCTL" --assess --type execute --verbose=2 "$target" > "$WORK/assess.out" 2>&1 ;;
dmg) "$SPCTL" --assess --type open --context context:primary-signature --verbose=2 "$target" > "$WORK/assess.out" 2>&1 ;;
esac || { cat "$WORK/assess.out" >&2; die "Gatekeeper rejects $(basename "$target") even though it was notarized"; }
say "Gatekeeper accepts $(basename "$target"): $(tr '\n' ' ' < "$WORK/assess.out" | sed 's/[[:space:]]*$//')"
