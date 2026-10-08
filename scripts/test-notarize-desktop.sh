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

# ---- the secrets a release needs, checked before anything is built
echo "check-release-secrets.sh"
printf 'KEYKEYKEYKEYKEYKEYKEYKEYKEYKEYKEYKEYKEYKEY' > "$WORK/pubkey"
release_secrets_env() { # <product kind> [NAME=value …]: disposable fixtures, never real signing credentials
  kind=$1; shift
  env -i PATH="$PATH" SPARKLE_PUBLIC_KEY_FILE="$WORK/pubkey" \
    APPLE_CERTIFICATE_P12=eA== APPLE_CERTIFICATE_PASSWORD=pw APPLE_NOTARY_KEY="-----BEGIN PRIVATE KEY-----
abc
-----END PRIVATE KEY-----" NOTARY_KEY_ID=ABCDE12345 NOTARY_ISSUER=11111111-2222-3333-4444-555555555555 \
    SPARKLE_ED_PRIVATE_KEY=c2VlZHNlZWRzZWVkc2VlZHNlZWRzZWVkc2VlZHNlZWQ= "$@" scripts/check-release-secrets.sh "$kind" 2>&1
}
secrets_env() { release_secrets_env desktop "$@"; }
RC=0; OUT=$(secrets_env) || RC=$?
[ $RC = 0 ] || bad "a release with every secret was refused" "$OUT"
RC=0; OUT=$(env -i PATH="$PATH" SPARKLE_PUBLIC_KEY_FILE="$WORK/none" scripts/check-release-secrets.sh desktop 2>&1) || RC=$?
[ $RC -ne 0 ] || bad "a release with no secret was accepted"
for name in APPLE_CERTIFICATE_P12 APPLE_CERTIFICATE_PASSWORD APPLE_NOTARY_KEY APPLE_NOTARY_KEY_ID APPLE_NOTARY_ISSUER SPARKLE_ED_PRIVATE_KEY sparkle-public-key; do
  contains "$OUT" "$name" || bad "no mention of $name when nothing is set" "$OUT"
done
for step in "step 5" "step 6"; do contains "$OUT" "$step" || bad "the message does not say which checklist step makes them ($step)" "$OUT"; done
# each one alone
for var in APPLE_CERTIFICATE_P12 APPLE_CERTIFICATE_PASSWORD APPLE_NOTARY_KEY NOTARY_KEY_ID NOTARY_ISSUER SPARKLE_ED_PRIVATE_KEY; do
  RC=0; OUT=$(secrets_env "$var=") || RC=$?
  [ $RC -ne 0 ] && contains "$OUT" "missing secrets" || bad "$var empty must be reported as missing" "$OUT"
done
# present but the wrong thing
for pair in "APPLE_CERTIFICATE_P12=%%%not-base64%%%:not base64" "APPLE_NOTARY_KEY=just a password:the text of the .p8" "NOTARY_KEY_ID=short:Key ID" "NOTARY_ISSUER=not-a-uuid:Issuer ID" "SPARKLE_ED_PRIVATE_KEY=short:one line of base64"; do
  RC=0; OUT=$(secrets_env "${pair%%:*}") || RC=$?
  [ $RC -ne 0 ] && contains "$OUT" "${pair#*:}" || bad "${pair%%=*} that is the wrong thing was accepted" "$OUT"
done
case $(secrets_env "APPLE_CERTIFICATE_PASSWORD=hunter2-must-not-show" SPARKLE_ED_PRIVATE_KEY=short) in *hunter2-must-not-show*|*c2VlZHNlZWRz*) bad "a secret was printed" ;; esac
ok "a release names every missing secret and the step that makes it, notices the wrong thing in a present one, and prints no value"

# Team must diagnose every missing credential before importing a certificate. It needs the license public key,
# independently of the individual updater's keys, and never needs the issuer's private signing key.
team_secrets_env() {
  release_secrets_env team-desktop SPARKLE_PUBLIC_KEY_FILE="$WORK/none" SPARKLE_ED_PRIVATE_KEY= \
    LICENSE_ISSUER_PUBLIC_KEY=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA "$@"
}
RC=0; OUT=$(team_secrets_env) || RC=$?
[ $RC = 0 ] || bad "Team credentials were refused without individual updater keys" "$OUT"
RC=0; OUT=$(env -i PATH="$PATH" scripts/check-release-secrets.sh team-desktop 2>&1) || RC=$?
[ $RC -ne 0 ] || bad "Team with no credentials was accepted"
for name in APPLE_CERTIFICATE_P12 APPLE_CERTIFICATE_PASSWORD APPLE_NOTARY_KEY APPLE_NOTARY_KEY_ID APPLE_NOTARY_ISSUER TEAM_LICENSE_ISSUER_PUBLIC_KEY docs/TEAM_DESKTOP.md; do
  contains "$OUT" "$name" || bad "Team did not report $name when no credentials are set" "$OUT"
done
case "$OUT" in *SPARKLE*|*sparkle-public-key*|*docs/DESKTOP_RELEASE.md*) bad "Team was given individual release requirements" "$OUT" ;; esac
for var in APPLE_CERTIFICATE_P12 APPLE_CERTIFICATE_PASSWORD APPLE_NOTARY_KEY NOTARY_KEY_ID NOTARY_ISSUER LICENSE_ISSUER_PUBLIC_KEY; do
  RC=0; OUT=$(team_secrets_env "$var=") || RC=$?
  [ $RC -ne 0 ] && contains "$OUT" "missing secrets" || bad "Team must reject missing $var" "$OUT"
done
for pair in "APPLE_CERTIFICATE_P12=%%%not-base64%%%:not base64" "APPLE_NOTARY_KEY=just a password:the text of the .p8" "NOTARY_KEY_ID=short:Key ID" "NOTARY_ISSUER=not-a-uuid:Issuer ID" "LICENSE_ISSUER_PUBLIC_KEY=short:32-byte Ed25519" "LICENSE_ISSUER_PUBLIC_KEY=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=:raw URL base64" "LICENSE_ISSUER_PUBLIC_KEY=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA+:raw URL base64"; do
  RC=0; OUT=$(team_secrets_env "${pair%%:*}") || RC=$?
  [ $RC -ne 0 ] && contains "$OUT" "${pair#*:}" || bad "Team accepted malformed ${pair%%=*}" "$OUT"
done
case $(team_secrets_env APPLE_CERTIFICATE_PASSWORD=team-password-must-not-show LICENSE_ISSUER_PUBLIC_KEY=short) in
  *team-password-must-not-show*|*eA==*|*BEGIN\ PRIVATE\ KEY*) bad "Team printed a credential" ;;
esac
ok "Team checks all Apple credentials and its license issuer public key, without updater keys or secret values"

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

# A release page with a disk image and an update archive that have what a real one has, signed through the recorder.
# release <dir> <version the app says> <version the program says>; the environment can spoil one thing:
#   FEED=<url>  the update feed the app holds     NOZIP=1  no update archive     OTHERZIP=1  an archive of another app
SPARKLE_FIXTURE_KEY=$(head -c 32 /dev/urandom | base64)
printf '%s' "$SPARKLE_FIXTURE_KEY" > "$WORK/sparkle-public-key"
export SPARKLE_PUBLIC_KEY_FILE="$WORK/sparkle-public-key"
sparkle_version=$(. "$ROOT/desktop/build/sparkle.env" && printf '%s' "$SPARKLE_VERSION")
release() {
  d=$1; shift
  rel="$d/werkbord-v1.2.3"; mkdir -p "$rel" "$d/src/Werkbord.app/Contents/MacOS" "$d/src/Werkbord.app/Contents/Helpers" "$d/src/Werkbord.app/Contents/Frameworks" "$d/disk"
  a="$d/src/Werkbord.app"
  printf 'int main(void){return 7;}\n' > "$d/window.c"
  printf '#include <stdio.h>\nint main(int c,char**v){puts("v%s");return 0;}\n' "$2" > "$d/program.c"
  clang -arch arm64 -arch x86_64 "$d/window.c" -o "$a/Contents/MacOS/Werkbord"
  clang -arch arm64 -arch x86_64 "$d/program.c" -o "$a/Contents/Helpers/werkbord"
  cp desktop/build/darwin/Info.plist "$a/Contents/Info.plist"; sed -i '' "s/@VERSION@/$1/" "$a/Contents/Info.plist"
  # what a release's Info.plist says about updating (scripts/check-desktop-updater.sh)
  PB=/usr/libexec/PlistBuddy; P="$a/Contents/Info.plist"
  $PB -c "Add :SUFeedURL string ${FEED:-https://github.com/micho8cho93/werkbord/releases/latest/download/appcast.xml}" "$P"
  $PB -c "Add :SUPublicEDKey string $SPARKLE_FIXTURE_KEY" "$P"
  for k in SUEnableAutomaticChecks SUSendProfileInfo SUAllowsAutomaticUpdates SUAutomaticallyUpdate; do $PB -c "Add :$k bool false" "$P"; done
  for k in SUVerifyUpdateBeforeExtraction SURequireSignedFeed; do $PB -c "Add :$k bool true" "$P"; done
  # a framework shaped like Sparkle's: a bundle with a version, signed before the app that holds it
  fw="$a/Contents/Frameworks/Sparkle.framework"; mkdir -p "$fw/Versions/B/Resources"
  printf 'int main(void){return 1;}\n' > "$d/fw.c"; clang -arch arm64 -arch x86_64 "$d/fw.c" -o "$fw/Versions/B/Sparkle"
  printf '<?xml version="1.0" encoding="UTF-8"?>\n<plist version="1.0"><dict><key>CFBundleExecutable</key><string>Sparkle</string><key>CFBundleIdentifier</key><string>org.sparkle-project.Sparkle</string><key>CFBundlePackageType</key><string>FMWK</string><key>CFBundleShortVersionString</key><string>%s</string><key>CFBundleVersion</key><string>2</string></dict></plist>\n' "$sparkle_version" > "$fw/Versions/B/Resources/Info.plist"
  ln -s B "$fw/Versions/Current"; ln -s Versions/Current/Sparkle "$fw/Sparkle"; ln -s Versions/Current/Resources "$fw/Resources"
  /usr/bin/codesign --remove-signature "$a/Contents/MacOS/Werkbord" "$a/Contents/Helpers/werkbord" "$fw/Versions/B/Sparkle"
  "$SHIM" --force --sign "$ID" --options runtime --timestamp=secure "$fw"
  "$SHIM" --force --sign "$ID" --options runtime --timestamp=secure "$a/Contents/Helpers/werkbord"
  "$SHIM" --force --sign "$ID" --options runtime --timestamp=secure --entitlements desktop/build/darwin/entitlements.plist "$a"
  ditto "$a" "$d/disk/Werkbord.app"; ln -s /Applications "$d/disk/Applications"
  hdiutil create -quiet -volname Werkbord -srcfolder "$d/disk" -fs HFS+ -format UDZO -ov "$rel/Werkbord_1.2.3_darwin_universal.dmg"
  "$SHIM" --force --sign "$ID" --timestamp=secure "$rel/Werkbord_1.2.3_darwin_universal.dmg"
  (cd "$rel" && shasum -a 256 Werkbord_1.2.3_darwin_universal.dmg > Werkbord_1.2.3_darwin_universal.dmg.sha256)
  cp "$rel/Werkbord_1.2.3_darwin_universal.dmg" "$rel/Werkbord.dmg" # the same file with no version in its name
  if [ -z "${NOZIP:-}" ]; then
    src=$a
    if [ -n "${OTHERZIP:-}" ]; then
      # an archive of an app that is not the one in the disk image: the same app with one more file, signed again
      src="$d/other/Werkbord.app"; mkdir -p "$d/other"
      ditto "$a" "$src"
      mkdir -p "$src/Contents/Resources"; echo different > "$src/Contents/Resources/x.txt"
      "$SHIM" --force --sign "$ID" --options runtime --timestamp=secure --entitlements desktop/build/darwin/entitlements.plist "$src"
    fi
    ditto -c -k --sequesterRsrc --keepParent "$src" "$rel/Werkbord_1.2.3_darwin_universal.zip"
    (cd "$rel" && shasum -a 256 Werkbord_1.2.3_darwin_universal.zip > Werkbord_1.2.3_darwin_universal.zip.sha256)
  fi
}
verify() { # <dir> [tag]: runs the check against the release page in <dir>, with Apple's tools as fakes that accept
  d=$1; tag=${2:-werkbord-v1.2.3}
  RC=0; OUT=$(FAKE_MODE=accepted CODESIGN="$SHIM" WERKBORD_RELEASE_BASE="file://$d" scripts/verify-desktop-release.sh "$tag" 2>&1) || RC=$?
}

release "$WORK/good" 1.2.3 1.2.3
verify "$WORK/good"
[ $RC = 0 ] || bad "a good release was rejected" "$OUT"
for want in "sha256 matches" "signed by the Developer ID" "Gatekeeper accepts the disk image" "stapled" "Notarized Developer ID" "both 1.2.3" "update archive holds the same notarized app" "Werkbord.dmg, the link with no version in it, is this disk image" "is a good release"; do
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

(NOZIP=1; release "$WORK/nozip" 1.2.3 1.2.3)
verify "$WORK/nozip"
[ $RC -ne 0 ] && contains "$OUT" "no update archive" || bad "a release whose app cannot be updated (no archive) must fail" "$OUT"
(OTHERZIP=1; release "$WORK/otherzip" 1.2.3 1.2.3)
verify "$WORK/otherzip"
[ $RC -ne 0 ] && contains "$OUT" "not the app in the disk image" || bad "an archive of another app than the disk image's must fail" "$OUT"
(FEED=https://example.com/appcast.xml; release "$WORK/wrongfeed" 1.2.3 1.2.3)
verify "$WORK/wrongfeed"
[ $RC -ne 0 ] && contains "$OUT" "feed is https://example.com/appcast.xml" || bad "an app that looks for updates somewhere else must fail" "$OUT"
(FEED=http://127.0.0.1:9/appcast.xml; release "$WORK/testfeed" 1.2.3 1.2.3)
verify "$WORK/testfeed"
[ $RC -ne 0 ] && contains "$OUT" "a release uses the one https address" || bad "an app that looks for updates on a local server must fail" "$OUT"
cp -R "$WORK/good" "$WORK/corruptzip"; printf 'x' >> "$WORK/corruptzip/werkbord-v1.2.3/Werkbord_1.2.3_darwin_universal.zip"
verify "$WORK/corruptzip"
[ $RC -ne 0 ] && contains "$OUT" "update archive does not match its checksum" || bad "a corrupt update archive must fail" "$OUT"
ok "an update archive that is missing, corrupt, of another app, or whose app looks for updates in the wrong place, fails"
cp -R "$WORK/good" "$WORK/noalias"; rm "$WORK/noalias/werkbord-v1.2.3/Werkbord.dmg"
verify "$WORK/noalias"
[ $RC -ne 0 ] && contains "$OUT" "no Werkbord.dmg" || bad "a release without the download link that never changes must fail" "$OUT"
cp -R "$WORK/good" "$WORK/otheralias"; printf 'x' >> "$WORK/otheralias/werkbord-v1.2.3/Werkbord.dmg"
verify "$WORK/otheralias"
[ $RC -ne 0 ] && contains "$OUT" "Werkbord.dmg is not the disk image" || bad "a Werkbord.dmg that is not the disk image must fail" "$OUT"
ok "Werkbord.dmg, the link with no version in its name, must exist and be the very same disk image"

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
