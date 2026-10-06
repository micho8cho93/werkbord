#!/bin/sh
# Tests scripts/notarize-desktop.sh and scripts/verify-desktop-release.sh without Apple and without a
# release: every program they run (xcrun, spctl, ditto, curl) is replaced by one that behaves as Apple's
# does in each situation, and records what it was asked.
#
#   scripts/test-notarize-desktop.sh
#
# notarize-desktop.sh: the success path, Apple rejecting the submission (its log is shown and the exit is
# non-zero, whatever notarytool's own exit status says), a submission that never starts, wrong credentials,
# a ticket that will not staple, Gatekeeper refusing, and no credentials at all. And that no
# credential ever appears in what it prints.
# verify-desktop-release.sh (macOS only): a good release, and each way a release can be wrong.
set -eu

cd "$(dirname "$0")/.."
ROOT=$(pwd)
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT INT TERM
pass=0
ok() { pass=$((pass + 1)); printf '  ok  %s\n' "$1"; }
bad() { printf 'FAIL  %s\n' "$1" >&2; [ -n "${2:-}" ] && printf '%s\n' "$2" >&2; exit 1; }
contains() { case "$1" in *"$2"*) return 0 ;; esac; return 1; }

# ---------------------------------------------------------------- the fakes
FAKE="$WORK/fake"; mkdir -p "$FAKE"
export FAKE_LOG="$WORK/calls.log"
cat > "$FAKE/ditto" <<'EOF'
#!/bin/sh
echo "ditto $*" >> "$FAKE_LOG"
# ditto -c -k --keepParent <src> <zip>
eval "last=\${$#}"; echo zip > "$last"
EOF
chmod +x "$FAKE"/*
export XCRUN="$ROOT/scripts/test-support/fake-xcrun.sh" SPCTL="$ROOT/scripts/test-support/fake-spctl.sh" DITTO="$FAKE/ditto" NOTARY_RETRY_SLEEP=0
export FAKE_COUNT="$WORK/count"

export FAKE_KEY="$WORK/AuthKey_KEYIDSENTINEL99.p8"
printf -- '-----BEGIN PRIVATE KEY-----\nSECRET-KEY-CONTENT-DO-NOT-PRINT\n-----END PRIVATE KEY-----\n' > "$FAKE_KEY"
export NOTARY_KEY_FILE="$FAKE_KEY" NOTARY_KEY_ID=KEYIDSENTINEL99 NOTARY_ISSUER=ISSUERSENTINEL99

mkdir -p "$WORK/Werkbord.app/Contents"; echo x > "$WORK/Werkbord.app/Contents/x"
echo dmg > "$WORK/Werkbord_1.2.3_darwin_universal.dmg"
APP="$WORK/Werkbord.app"; DMG="$WORK/Werkbord_1.2.3_darwin_universal.dmg"

run() { # <mode> <args…>: runs notarize-desktop.sh with Apple behaving as <mode>, output in $OUT, status in $RC
  mode=$1; shift
  : > "$FAKE_LOG"; : > "$FAKE_COUNT"
  RC=0; OUT=$(FAKE_MODE=$mode scripts/notarize-desktop.sh "$@" 2>&1) || RC=$?
}
calls() { cat "$FAKE_LOG"; }
no_secrets() { # the output never holds a credential
  for s in KEYIDSENTINEL99 ISSUERSENTINEL99 SECRET-KEY-CONTENT-DO-NOT-PRINT "$FAKE_KEY"; do
    contains "$OUT" "$s" && bad "$1: the output contains a credential ($s)" "$OUT"
  done
  return 0
}

echo "notarize-desktop.sh"
# ---- accepted
run accepted app "$APP"
[ $RC = 0 ] || bad "accepted app: exit $RC" "$OUT"
contains "$(calls)" "notarytool submit" || bad "nothing was submitted"
c=$(calls); order=$(printf '%s\n' "$c" | awk '{ if ($1 == "xcrun") print $1 " " $2; else print $1 }' | tr '\n' ',')
[ "$order" = "ditto,xcrun notarytool,xcrun stapler,xcrun stapler,spctl," ] || bad "order: zip, submit, staple, validate, assess" "$order
$c"
contains "$c" "notarytool submit /" && contains "$c" "Werkbord.zip --wait" || bad "an app is submitted as a zip" "$c"
contains "$c" "--wait" && contains "$c" "--output-format json" || bad "it waits for Apple's verdict and reads it as JSON" "$c"
contains "$c" "--key $FAKE_KEY --key-id KEYIDSENTINEL99 --issuer ISSUERSENTINEL99" || bad "the API key (not an Apple ID password) is what authenticates" "$c"
contains "$c" "password" && bad "an Apple ID password is not used" "$c"
contains "$c" "stapler staple $APP" && contains "$c" "stapler validate $APP" || bad "the ticket is stapled to the app and validated" "$c"
contains "$c" "spctl --assess --type execute" || bad "Gatekeeper is asked about the app as something to run" "$c"
no_secrets "accepted app"
ok "an accepted app: zipped, submitted with the API key, waited for, stapled, validated, assessed, in that order"

run accepted dmg "$DMG"
[ $RC = 0 ] || bad "accepted dmg: exit $RC" "$OUT"
c=$(calls)
contains "$c" "submit $DMG " || bad "a disk image is submitted as it is" "$c"
contains "$c" "ditto" && bad "a disk image is not zipped" "$c"
contains "$c" "spctl --assess --type open --context context:primary-signature" || bad "Gatekeeper is asked about the image as a document that opens" "$c"
contains "$c" "stapler staple $DMG" || bad "the ticket is stapled to the image" "$c"
no_secrets "accepted dmg"
ok "an accepted disk image: submitted as it is, stapled, assessed with the primary-signature context"

# ---- rejected: Apple's log is shown, the exit is not zero, and nothing is stapled; whatever notarytool's exit status says
for mode in invalid invalid-exit1; do
  run $mode app "$APP"
  [ $RC -ne 0 ] || bad "$mode: a rejected submission exited 0" "$OUT"
  contains "$OUT" "The signature of the binary is invalid" || bad "$mode: Apple's log is not shown" "$OUT"
  contains "$OUT" "Contents/Helpers/werkbord" || bad "$mode: the log does not name the file" "$OUT"
  contains "$(calls)" "notarytool log bbbbbbbb-1111-2222-3333-444444444444" || bad "$mode: the log of that submission was not fetched" "$(calls)"
  contains "$(calls)" "stapler" && bad "$mode: something was stapled after a rejection" "$(calls)"
  no_secrets "$mode"
done
ok "a rejected submission shows Apple's log, fails, and staples nothing (also when notarytool itself exits 0)"

# ---- the submission never starts
run flaky dmg "$DMG"
[ $RC = 0 ] || bad "a network that recovers: exit $RC" "$OUT"
[ "$(calls | grep -c 'notarytool submit')" = 3 ] || bad "it should have submitted three times" "$(calls)"
ok "a submission that fails to start is retried, and then goes through"
run down dmg "$DMG"
[ $RC -ne 0 ] || bad "no network at all must fail" "$OUT"
[ "$(calls | grep -c 'notarytool submit')" = 3 ] || bad "it gives up after three attempts" "$(calls)"
contains "$OUT" "after 3 attempts" || bad "it says it gave up" "$OUT"
ok "with no network it gives up after three attempts, loudly"
run unauthorized dmg "$DMG"
[ $RC -ne 0 ] || bad "wrong credentials must fail" "$OUT"
contains "$OUT" "Apple refused the credentials" || bad "wrong credentials are named" "$OUT"
[ "$(calls | grep -c 'notarytool submit')" = 1 ] || bad "wrong credentials are not retried" "$(calls)"
no_secrets "unauthorized (Apple's error echoes the key id, issuer and key file)"
ok "wrong credentials fail at once, and what Apple echoes of them is scrubbed"

# ---- after acceptance
run staple-fails dmg "$DMG"
[ $RC -ne 0 ] && contains "$OUT" "could not staple" || bad "a ticket that will not staple fails" "$OUT"
run validate-fails dmg "$DMG"
[ $RC -ne 0 ] && contains "$OUT" "does not validate" || bad "a ticket that does not validate fails" "$OUT"
run gatekeeper-rejects app "$APP"
[ $RC -ne 0 ] && contains "$OUT" "Gatekeeper rejects" || bad "Gatekeeper refusing fails" "$OUT"
ok "a ticket that will not staple or validate, or a Gatekeeper that refuses, each fail"

# ---- credentials
for var in NOTARY_KEY_FILE NOTARY_KEY_ID NOTARY_ISSUER; do
  : > "$FAKE_LOG"
  RC=0; OUT=$(env -u "$var" scripts/notarize-desktop.sh app "$APP" 2>&1) || RC=$?
  [ $RC = 0 ] || bad "$var missing: the step must exit cleanly" "$OUT"
  [ "$(printf '%s\n' "$OUT" | wc -l | tr -d ' ')" = 1 ] && contains "$OUT" "$var" || bad "$var missing: one line that names it" "$OUT"
  [ ! -s "$FAKE_LOG" ] || bad "$var missing: Apple was contacted anyway" "$(calls)"
  RC=0; OUT=$(env -u "$var" scripts/notarize-desktop.sh --require app "$APP" 2>&1) || RC=$?
  [ $RC -ne 0 ] && contains "$OUT" "$var" || bad "$var missing with --require must fail loudly and say which" "$OUT"
done
RC=0; OUT=$(env -u NOTARY_KEY_FILE -u NOTARY_KEY_ID -u NOTARY_ISSUER scripts/notarize-desktop.sh app "$APP" 2>&1) || RC=$?
contains "$OUT" "NOTARY_KEY_FILE, NOTARY_KEY_ID, NOTARY_ISSUER" || bad "all three are named when all are missing" "$OUT"
RC=0; OUT=$(NOTARY_KEY_FILE="$WORK/nothing.p8" scripts/notarize-desktop.sh app "$APP" 2>&1) || RC=$?
[ $RC -ne 0 ] && contains "$OUT" "readable file" || bad "a key file that is not there is explained" "$OUT"
ok "with no credentials it names the missing ones on one line and exits 0 (1 with --require), and contacts nobody"

# ---------------------------------------------------------------- the check of a published release
if [ "$(uname -s)" != Darwin ]; then
  echo "(verify-desktop-release.sh is checked on macOS only)"
  echo "ok: $pass checks"
  exit 0
fi
echo "verify-desktop-release.sh"
SHIM_DIR="$WORK/shim"; mkdir -p "$SHIM_DIR/records"; export SHIM_DIR
SHIM="$ROOT/scripts/test-support/fake-codesign.sh"
ID="Developer ID Application: Werkbord Test (TEST123456)"

# A release page with a disk image that has what a real one has, signed through the recorder.
release() { # <dir> <version the app says> <version the program says>
  d=$1; shift
  rel="$d/werkbord-v1.2.3"; mkdir -p "$rel" "$d/src/Werkbord.app/Contents/MacOS" "$d/src/Werkbord.app/Contents/Helpers" "$d/disk"
  a="$d/src/Werkbord.app"
  printf 'int main(void){return 7;}\n' > "$d/window.c"
  printf '#include <stdio.h>\nint main(int c,char**v){puts("v%s");return 0;}\n' "$2" > "$d/program.c"
  clang -arch arm64 -arch x86_64 "$d/window.c" -o "$a/Contents/MacOS/Werkbord"
  clang -arch arm64 -arch x86_64 "$d/program.c" -o "$a/Contents/Helpers/werkbord"
  cp desktop/build/darwin/Info.plist "$a/Contents/Info.plist"; sed -i '' "s/@VERSION@/$1/" "$a/Contents/Info.plist"
  /usr/bin/codesign --remove-signature "$a/Contents/MacOS/Werkbord" "$a/Contents/Helpers/werkbord"
  "$SHIM" --force --sign "$ID" --options runtime --timestamp=secure "$a/Contents/Helpers/werkbord"
  "$SHIM" --force --sign "$ID" --options runtime --timestamp=secure --entitlements desktop/build/darwin/entitlements.plist "$a"
  ditto "$a" "$d/disk/Werkbord.app"; ln -s /Applications "$d/disk/Applications"
  hdiutil create -quiet -volname Werkbord -srcfolder "$d/disk" -fs HFS+ -format UDZO -ov "$rel/Werkbord_1.2.3_darwin_universal.dmg"
  "$SHIM" --force --sign "$ID" --timestamp=secure "$rel/Werkbord_1.2.3_darwin_universal.dmg"
  (cd "$rel" && shasum -a 256 Werkbord_1.2.3_darwin_universal.dmg > Werkbord_1.2.3_darwin_universal.dmg.sha256)
}
verify() { # <dir> [tag]: runs the check against the release page in <dir>, with Apple's tools as fakes that accept
  d=$1; tag=${2:-werkbord-v1.2.3}
  RC=0; OUT=$(FAKE_MODE=accepted CODESIGN="$SHIM" WERKBORD_RELEASE_BASE="file://$d" scripts/verify-desktop-release.sh "$tag" 2>&1) || RC=$?
}

release "$WORK/good" 1.2.3 1.2.3
verify "$WORK/good"
[ $RC = 0 ] || bad "a good release was rejected" "$OUT"
for want in "sha256 matches" "signed by the Developer ID" "Gatekeeper accepts the disk image" "stapled" "Notarized Developer ID" "both 1.2.3" "is a good release"; do
  contains "$OUT" "$want" || bad "the report does not say \"$want\"" "$OUT"
done
ok "a good release passes every check, and the report says what was checked"
contains "$(calls)" "stapler validate" && contains "$(calls)" "spctl --assess --type execute" || bad "the staple and Gatekeeper were asked about"

release "$WORK/wrongprogram" 1.2.3 1.2.4
verify "$WORK/wrongprogram"
[ $RC -ne 0 ] && contains "$OUT" "program inside the app says v1.2.4" || bad "a program of another version must fail" "$OUT"
release "$WORK/wrongapp" 1.2.2 1.2.3
verify "$WORK/wrongapp"
[ $RC -ne 0 ] && contains "$OUT" "app says it is version 1.2.2" || bad "an app of another version must fail" "$OUT"
ok "the app's and the program's version must both be the tag's"

cp -R "$WORK/good" "$WORK/corrupt"
printf 'x' >> "$WORK/corrupt/werkbord-v1.2.3/Werkbord_1.2.3_darwin_universal.dmg"
verify "$WORK/corrupt"
[ $RC -ne 0 ] && contains "$OUT" "sha256" || bad "a disk image that is not what the checksum says must fail" "$OUT"
cp -R "$WORK/good" "$WORK/nosum"; rm "$WORK/nosum/werkbord-v1.2.3/Werkbord_1.2.3_darwin_universal.dmg.sha256"
verify "$WORK/nosum"
[ $RC -ne 0 ] && contains "$OUT" "no checksum" || bad "a release without a checksum must fail" "$OUT"
cp -R "$WORK/good" "$WORK/nodmg"; rm "$WORK/nodmg/werkbord-v1.2.3/Werkbord_1.2.3_darwin_universal.dmg"
verify "$WORK/nodmg"
[ $RC -ne 0 ] && contains "$OUT" "no disk image" || bad "a release without a disk image must fail" "$OUT"
ok "a corrupt image, a missing checksum and a missing image each fail"

verify "$WORK/good" werkbord-team-v1.2.3
[ $RC -ne 0 ] && contains "$OUT" "Werkbord Team" || bad "a Team tag must be refused" "$OUT"
verify "$WORK/good" v1.2.3
[ $RC -ne 0 ] || bad "a bare tag must be refused" "$OUT"
ok "only an individual release tag is accepted"

# an app signed ad hoc (what a local build is) is not a release, however well it is packaged
release "$WORK/adhoc" 1.2.3 1.2.3
RC=0; OUT=$(FAKE_MODE=accepted WERKBORD_RELEASE_BASE="file://$WORK/adhoc" scripts/verify-desktop-release.sh werkbord-v1.2.3 2>&1) || RC=$? # the real codesign: the fake identity is not a certificate
[ $RC -ne 0 ] || bad "a release checked with the real codesign and no certificate must fail" "$OUT"
ok "with the real codesign, a disk image that is not Developer ID signed fails"
# and the real Gatekeeper has the last word
RC=0; OUT=$(FAKE_MODE=accepted CODESIGN="$SHIM" SPCTL=spctl WERKBORD_RELEASE_BASE="file://$WORK/good" scripts/verify-desktop-release.sh werkbord-v1.2.3 2>&1) || RC=$?
[ $RC -ne 0 ] && contains "$OUT" "Gatekeeper rejects" || bad "the real Gatekeeper must reject an un-notarized image" "$OUT"
ok "the real Gatekeeper rejects an image that Apple never saw: it is asked, and its answer decides"

echo "ok: $pass checks"
