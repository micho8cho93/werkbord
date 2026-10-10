#!/bin/sh
# Tests how the desktop app is signed, without a Developer ID and without touching the computer it runs on:
#
#   scripts/test-desktop-sign.sh            everything (builds the app: a few minutes)
#   scripts/test-desktop-sign.sh --fast     the checks that need no build
#
# What it proves, and what it cannot:
#
#  1. scripts/ci-keychain.sh makes a temporary keychain, imports an identity into it, finds it, leaves the
#     user's keychain list alone, removes the decoded certificate, and deletes everything, including after
#     a failure. It uses a self-signed certificate made here. (macOS will not let `codesign` USE such a
#     certificate unless it is added to the trust settings, which a test must not change, so the
#     certificate is imported and found, not signed with.)
#  2. scripts/check-desktop-signature.sh accepts a bundle signed the way a release is and rejects each
#     way that is not: a piece of code left unsigned, ad hoc, without the hardened runtime, without a
#     timestamp, from another team, with an entitlement nobody reviewed.
#  3. scripts/build-desktop.sh --release refuses everything that could publish something unsigned or
#     test-signed, before it builds anything.
#  4. The real build, with a Developer-ID-shaped identity, signs inside-out with the hardened runtime, in
#     the order and with the flags a release has. `codesign` is a recorder (SHIM below) that does the
#     real hardened-runtime signing, ad hoc, and reports what a certificate would have added. That is
#     exactly the part this machine cannot do for real; everything else about the signature is real.
#  5. The signed program and the signed app RUN under the hardened runtime with the entitlements file
#     as it is (none): the program reports its version, a controller starts from it, the launcher's
#     end-to-end scenarios pass on hardened binaries, the embedded Tailscale node starts, and the signed
#     app opens a window and connects.
#
# Nothing here uses the installed Werkbord: every controller has a temporary HOME, data directory and a
# free port, and no login service is made.
set -eu
# Isolated Individual signature/updater fixtures. Unified payloads are tested by
# test-unified-installer.sh; production --release rejects this override.
export TEAM_PAYLOAD=0

FAST=""
[ "${1:-}" != --fast ] || FAST=1

cd "$(dirname "$0")/.."
ROOT=$(pwd)
[ "$(uname -s)" = Darwin ] || { echo "test-desktop-sign: needs macOS"; exit 0; }

WORK=$(mktemp -d)
cleanup() {
  [ -z "${APP_PID:-}" ] || kill "$APP_PID" 2>/dev/null || true
  for d in "$WORK"/home*/data; do
    [ -f "$d/controller.pid" ] && kill "$(cat "$d/controller.pid")" 2>/dev/null || true
  done
  # the keychains this test made, if a failure left one
  for k in "$WORK"/keychains*/werkbord-signing.keychain-db; do
    [ -e "$k" ] && security delete-keychain "$k" >/dev/null 2>&1 || true
  done
  rm -rf "$WORK"
}
trap cleanup EXIT INT TERM

pass=0
ok() { pass=$((pass + 1)); printf '  ok  %s\n' "$1"; }
bad() { printf 'FAIL  %s\n' "$1" >&2; [ -n "${2:-}" ] && printf '%s\n' "$2" >&2; exit 1; }
contains() { case "$1" in *"$2"*) return 0 ;; esac; return 1; }
expect_fail() { # <description> <expected text in the message> <command...>
  desc=$1; want=$2; shift 2
  if out=$("$@" 2>&1); then bad "$desc: it succeeded" "$out"; fi
  contains "$out" "$want" || bad "$desc: the message does not say \"$want\"" "$out"
  ok "$desc"
}
free_port() { python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])'; }

# The controllers already running on this computer (the person's own), and when each started: they must be the same afterwards.
running() { ps -axo pid=,lstart=,command= | grep '[w]erkbord serve' | grep -v "$WORK" || true; }
RUNNING_BEFORE=$(running)

# ---- the signed app, started as the Mac it was built on runs it, and (Rosetta, if there is any) as an Intel Mac does
run_app() { # <label> <prefix words…>: the app as that kind of Mac runs it
  label=$1; shift
  H2="$WORK/home-app-$label"; mkdir -p "$H2"
  PORT2=$(free_port)
  env2() { HOME="$H2" WERKBORD_DATA_DIR="$H2/data" DEVBOARD_DATA_DIR="" WERKBORD_ADDR="127.0.0.1:$PORT2" DEVBOARD_ADDR="" "$@"; }
  # A controller of its own (no login service) for the app to find and join: the app must not make one here.
  env2 "$@" "$APP/Contents/Helpers/werkbord" setup --no-service --no-open --no-network >/dev/null 2>&1 || bad "$label: could not set up the controller for the app"
  pid_before=$(cat "$H2/data/controller.pid")
  # The app itself is the background job (not a shell function around it), so that $! is its own process.
  HOME="$H2" WERKBORD_DATA_DIR="$H2/data" DEVBOARD_DATA_DIR="" WERKBORD_ADDR="127.0.0.1:$PORT2" DEVBOARD_ADDR="" \
    "$@" "$APP/Contents/MacOS/Werkbord" >"$WORK/app-$label.out" 2>&1 &
  APP_PID=$!
  n=0
  until grep -q 'connected' "$H2/Library/Logs/Werkbord/desktop.log" 2>/dev/null; do
    n=$((n + 1)); [ $n -lt 80 ] || { cat "$H2/Library/Logs/Werkbord/desktop.log" "$WORK/app-$label.out" 2>/dev/null >&2; bad "$label: the signed app did not connect"; }
    kill -0 "$APP_PID" 2>/dev/null || { cat "$H2/Library/Logs/Werkbord/desktop.log" "$WORK/app-$label.out" 2>/dev/null >&2; bad "$label: the signed app exited"; }
    sleep 0.5
  done
  cat > "$WORK/windows.js" <<'JXA'
ObjC.import("CoreGraphics");
function run(argv) {
  var pid = parseInt(argv[0], 10), list = ObjC.castRefToObject($.CGWindowListCopyWindowInfo(0, 0)), n = 0;
  for (var i = 0; i < list.count; i++) if (ObjC.unwrap(list.objectAtIndex(i).objectForKey("kCGWindowOwnerPID")) === pid) n++;
  return n;
}
JXA
  windows=$(osascript -l JavaScript "$WORK/windows.js" "$APP_PID" 2>/dev/null || echo "?")
  if [ "$windows" = "?" ] || [ "$windows" -le 0 ]; then
    # A CI runner may have no display session to put a window on: the app connected, which is what is being tested.
    [ -n "${CI:-}" ] || bad "$label: the signed app has no window (the window list says: $windows)" "$(cat "$H2/Library/Logs/Werkbord/desktop.log")"
    echo "  --  $label: no window was listed (CI has no display session to guarantee one)"
  fi
  started=$(grep 'msg=starting' "$H2/Library/Logs/Werkbord/desktop.log" | sed -n 's/.* arch=\([a-z0-9]*\).*/\1/p' | head -1)
  ok "$label: the hardened, signed app (running as $started) started, joined the controller, and left it alone (CoreGraphics lists $windows windows for it)"
  [ -z "$(ls "$H2/Library/LaunchAgents" 2>/dev/null)" ] || bad "$label: the app made a login service in the test home"
  # This build has no update key, so it carries no updater: it says so, asks the internet for nothing, and still has its one menu item.
  grep -q 'the app cannot update itself in this build' "$H2/Library/Logs/Werkbord/desktop.log" || bad "$label: a build with no update key did not say that it cannot update itself" "$(cat "$H2/Library/Logs/Werkbord/desktop.log")"
  grep -q 'added Check for Updates… to the application menu' "$H2/Library/Logs/Werkbord/desktop.log" || bad "$label: the Check for Updates… menu item was not added" "$(cat "$H2/Library/Logs/Werkbord/desktop.log")"
  [ "$(cat "$H2/data/controller.pid")" = "$pid_before" ] && kill -0 "$pid_before" 2>/dev/null || bad "$label: the app restarted or replaced the controller it joined"
  if grep -Eq 'phase=(installing|starting|upgrading)' "$H2/Library/Logs/Werkbord/desktop.log"; then bad "$label: the app installed, started or upgraded something when a controller was already answering" "$(cat "$H2/Library/Logs/Werkbord/desktop.log")"; fi
  kill "$APP_PID" 2>/dev/null || true; wait "$APP_PID" 2>/dev/null || true; APP_PID=""
  env2 "$@" "$APP/Contents/Helpers/werkbord" stop >/dev/null 2>&1 || true
}
run_apps() {
  if pgrep -x Werkbord >/dev/null 2>&1; then
    echo "  --  a Werkbord app is already open on this computer: not starting a second one (its single-instance lock is shared)"
  else
    run_app native
    if arch -x86_64 /usr/bin/true >/dev/null 2>&1; then
      case "$(uname -m)" in
        arm64) run_app intel arch -x86_64 ;;
      esac
    else
      echo "  --  no Rosetta here: the Intel half of the app is built and signed but was not run"
    fi
  fi
}

# APP_ONLY=<a Werkbord.app> runs just that part, on an app that is already built (a few minutes, not a long build)
if [ -n "${APP_ONLY:-}" ]; then
  APP=$APP_ONLY
  run_apps
  echo "ok: $pass checks (the app only)"
  exit 0
fi

# =================================================================== 1. the temporary keychain
echo "1. the temporary keychain"
mkdir -p "$WORK/keychains1"
cat > "$WORK/cert.cnf" <<'EOF'
[req]
distinguished_name = dn
x509_extensions = ext
prompt = no
[dn]
CN = Werkbord Test Signing
[ext]
basicConstraints = critical,CA:false
keyUsage = critical,digitalSignature
extendedKeyUsage = critical,codeSigning
EOF
openssl req -x509 -newkey rsa:2048 -nodes -keyout "$WORK/key.pem" -out "$WORK/cert.pem" -days 2 -config "$WORK/cert.cnf" >/dev/null 2>&1
P12PW=$(openssl rand -hex 12)
# 3DES/SHA1: what macOS's `security import` reads (OpenSSL 3's default encryption it does not).
openssl pkcs12 -export -inkey "$WORK/key.pem" -in "$WORK/cert.pem" -out "$WORK/id.p12" -passout "pass:$P12PW" -name "Werkbord Test Signing" \
  -keypbe PBE-SHA1-3DES -certpbe PBE-SHA1-3DES -macalg sha1 >/dev/null 2>&1
P12B64=$(base64 < "$WORK/id.p12" | tr -d '\n')

list_before=$(security list-keychains -d user)
default_before=$(security default-keychain -d user)
# The mode that leaves the user's keychain list alone, whatever environment this runs in (a CI runner sets GITHUB_ACTIONS, which is the other mode: below).
kcenv() { env -u GITHUB_ACTIONS -u GITHUB_ENV RUNNER_TEMP="$WORK/keychains1" CI_KEYCHAIN_ALLOW_UNTRUSTED=1 "$@"; }

# missing secrets: one clear line, clean exit; --require makes it a failure
out=$(env -u APPLE_CERTIFICATE_P12 -u APPLE_CERTIFICATE_PASSWORD scripts/ci-keychain.sh create 2>&1) || bad "create without secrets must exit cleanly" "$out"
contains "$out" "APPLE_CERTIFICATE_P12, APPLE_CERTIFICATE_PASSWORD is not set" || contains "$out" "APPLE_CERTIFICATE_P12" || bad "the missing secret is not named" "$out"
[ "$(printf '%s\n' "$out" | wc -l | tr -d ' ')" = 1 ] || bad "a missing secret is one line" "$out"
ok "no secrets: one line naming the secret, exit 0"
expect_fail "no secrets with --require fails loudly" "APPLE_CERTIFICATE_P12" env -u GITHUB_ACTIONS -u APPLE_CERTIFICATE_P12 -u APPLE_CERTIFICATE_PASSWORD scripts/ci-keychain.sh create --require
expect_fail "only the password missing is named" "APPLE_CERTIFICATE_PASSWORD" env -u APPLE_CERTIFICATE_PASSWORD APPLE_CERTIFICATE_P12=eA== scripts/ci-keychain.sh create --require

# a certificate that cannot be read leaves nothing behind
expect_fail "a wrong password is explained" "APPLE_CERTIFICATE_PASSWORD" kcenv env APPLE_CERTIFICATE_P12="$P12B64" APPLE_CERTIFICATE_PASSWORD=wrong scripts/ci-keychain.sh create
[ ! -e "$WORK/keychains1/werkbord-signing.keychain-db" ] || bad "a failed create left its keychain behind"
left=$(find "$WORK/keychains1" -mindepth 1 -maxdepth 1 -type d -name 'werkbord-signing.*'); [ -z "$left" ] || bad "a failed create left the decoded certificate behind: $left"
expect_fail "something that is not base64 is explained" "not base64" kcenv env APPLE_CERTIFICATE_P12='%%%' APPLE_CERTIFICATE_PASSWORD=x scripts/ci-keychain.sh create
ok "a failed create leaves no keychain and no certificate file"

out=$(kcenv env APPLE_CERTIFICATE_P12="$P12B64" APPLE_CERTIFICATE_PASSWORD="$P12PW" scripts/ci-keychain.sh create --require 2>&1) || bad "create failed" "$out"
contains "$out" "IDENTITY=Werkbord Test Signing" || bad "create did not report the identity" "$out"
KC="$WORK/keychains1/werkbord-signing.keychain-db"
[ -e "$KC" ] || bad "no keychain was made at the documented place"
contains "$(security find-identity -p codesigning "$KC")" "Werkbord Test Signing" || bad "the identity is not in the temporary keychain"
ok "the identity is imported into a temporary keychain and found"
left=$(find "$WORK/keychains1" -mindepth 1 -maxdepth 1 -type d -name 'werkbord-signing.*'); [ -z "$left" ] || bad "the decoded certificate was left behind: $left"
ok "the decoded certificate is gone as soon as it is imported"
[ "$list_before" = "$(security list-keychains -d user)" ] && [ "$default_before" = "$(security default-keychain -d user)" ] || bad "the user's keychain list or default keychain changed"
contains "$(security find-identity -p codesigning)" "Werkbord Test Signing" && bad "the identity leaked into the user's keychains"
ok "the user's keychains are not touched, and the identity is only in the temporary one"
case "$out" in *"$P12PW"*|*"$P12B64"*) bad "create printed a secret" ;; esac
ok "nothing secret is printed"

out=$(kcenv scripts/ci-keychain.sh delete 2>&1) || bad "delete failed" "$out"
[ ! -e "$KC" ] || bad "delete left the keychain"
contains "$(security list-keychains -d user)" "werkbord-signing" && bad "the keychain list still names the temporary keychain"
kcenv scripts/ci-keychain.sh delete >/dev/null 2>&1 || bad "a second delete must not fail"
ok "delete removes it, and deleting again is harmless"

# On a CI runner (and only there: it changes the keychain search list for a moment) the other mode: the temporary keychain is put in the
# list, so that Apple's intermediate certificates are found, GITHUB_ENV is told which keychain and identity to use, and delete puts it all back.
if [ -n "${CI:-}" ]; then
  mkdir -p "$WORK/keychains-ci"; : > "$WORK/ghenv"
  list_before_ci=$(security list-keychains -d user)
  out=$(RUNNER_TEMP="$WORK/keychains-ci" GITHUB_ACTIONS=true GITHUB_ENV="$WORK/ghenv" CI_KEYCHAIN_ALLOW_UNTRUSTED=1 \
    APPLE_CERTIFICATE_P12="$P12B64" APPLE_CERTIFICATE_PASSWORD="$P12PW" scripts/ci-keychain.sh create --require 2>&1) || bad "create in CI mode failed" "$out"
  contains "$(security list-keychains -d user)" "werkbord-signing.keychain-db" || bad "in CI the temporary keychain is put in the search list" "$(security list-keychains -d user)"
  contains "$(cat "$WORK/ghenv")" "CODESIGN_KEYCHAIN=" && contains "$(cat "$WORK/ghenv")" "CODESIGN_IDENTITY=Werkbord Test Signing" || bad "GITHUB_ENV was not told the keychain and the identity" "$(cat "$WORK/ghenv")"
  RUNNER_TEMP="$WORK/keychains-ci" GITHUB_ACTIONS=true scripts/ci-keychain.sh delete >/dev/null 2>&1 || bad "delete in CI mode failed"
  [ "$list_before_ci" = "$(security list-keychains -d user)" ] || bad "the keychain list was not put back after the job" "before:
$list_before_ci
after:
$(security list-keychains -d user)"
  ok "on a runner: the temporary keychain is in the list while the job runs, GITHUB_ENV names it, and deleting puts the list back exactly"
fi

# =================================================================== 2. the signature policy
echo "2. the signature policy"
SHIM_DIR="$WORK/shim"; mkdir -p "$SHIM_DIR/records"
SHIM="$ROOT/scripts/test-support/fake-codesign.sh" # the recorder: what it does, and what it cannot, is written at the top of that file
export SHIM_DIR
ID="Developer ID Application: Werkbord Test (TEST123456)"

# a small bundle with the same shape as the app: a window, a program in Helpers, and the Info.plist
fakeapp() { # <dir>: makes <dir>/Werkbord.app and prints nothing
  a=$1/Werkbord.app
  mkdir -p "$a/Contents/MacOS" "$a/Contents/Helpers" "$a/Contents/Resources"
  # Distinct programs, because a signature is known by the hash of what it seals.
  FAKEN=$((${FAKEN:-0} + 1))
  printf 'int main(void){return %d;}\n' "$FAKEN" | clang -x c - -o "$a/Contents/MacOS/Werkbord"
  printf 'int main(void){return %d;}\n' "$((FAKEN + 1000))" | clang -x c - -o "$a/Contents/Helpers/werkbord"
  /usr/bin/codesign --remove-signature "$a/Contents/MacOS/Werkbord" "$a/Contents/Helpers/werkbord"
  cp desktop/build/darwin/Info.plist "$a/Contents/Info.plist"
  sed -i '' 's/@VERSION@/1.2.3/' "$a/Contents/Info.plist"
}
signfake() { # <app> signs it the way the build does, through the recorder
  CODESIGN="$SHIM" "$SHIM" --force --sign "$ID" --options runtime --timestamp=secure "$1/Contents/Helpers/werkbord"
  CODESIGN="$SHIM" "$SHIM" --force --sign "$ID" --options runtime --timestamp=secure --entitlements desktop/build/darwin/entitlements.plist "$1"
}
check() { CODESIGN="$SHIM" scripts/check-desktop-signature.sh "$@"; }

fakeapp "$WORK/good"; signfake "$WORK/good/Werkbord.app"
check --distribution "$WORK/good/Werkbord.app" >/dev/null || bad "a bundle signed the way a release is was rejected" "$(check --distribution "$WORK/good/Werkbord.app" 2>&1)"
ok "a bundle signed the way a release is passes --distribution"

# each way a bundle can be wrong
broken() { # <name> <SHIM_BREAK> <expected message>
  fakeapp "$WORK/$1"
  SHIM_BREAK=$2 signfake "$WORK/$1/Werkbord.app"
  out=$(check --distribution "$WORK/$1/Werkbord.app" 2>&1) && bad "$1: a bundle that is $3 was accepted" "$out"
  contains "$out" "$4" || bad "$1: the message does not say \"$4\"" "$out"
  ok "--distribution rejects a bundle that is $3"
}
# (codesign itself refuses to seal a bundle that holds unsigned code, so "unsigned" is a signature that was lost afterwards)
fakeapp "$WORK/helper-unsigned"; signfake "$WORK/helper-unsigned/Werkbord.app"
/usr/bin/codesign --remove-signature "$WORK/helper-unsigned/Werkbord.app/Contents/Helpers/werkbord"
out=$(check --distribution "$WORK/helper-unsigned/Werkbord.app" 2>&1) && bad "a bundle whose program lost its signature was accepted" "$out"
contains "$out" "does not verify" || bad "the message for a lost signature" "$out"
ok "--distribution rejects a bundle whose program has lost its signature"
broken helper-adhoc "adhoc:Helpers/werkbord" "carrying an ad hoc signed program" "ad hoc"
broken helper-noruntime "noruntime:Helpers/werkbord" "carrying a program without the hardened runtime" "hardened runtime"
broken app-noruntime "noruntime:@Werkbord.app" "without the hardened runtime" "hardened runtime"
broken helper-notimestamp "notimestamp:Helpers/werkbord" "carrying a program with no secure timestamp" "timestamp"
broken helper-otherteam "team:Helpers/werkbord" "carrying a program of another team" "signed by team"
# an entitlement nobody reviewed
fakeapp "$WORK/ent"
cat > "$WORK/jit.plist" <<'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>com.apple.security.cs.allow-jit</key><true/></dict></plist>
EOF
"$SHIM" --force --sign "$ID" --options runtime --timestamp=secure "$WORK/ent/Werkbord.app/Contents/Helpers/werkbord"
"$SHIM" --force --sign "$ID" --options runtime --timestamp=secure --entitlements "$WORK/jit.plist" "$WORK/ent/Werkbord.app"
out=$(check --distribution "$WORK/ent/Werkbord.app" 2>&1) && bad "an entitlement outside the reviewed file was accepted" "$out"
contains "$out" "entitlements" || bad "the entitlement message" "$out"
ok "--distribution rejects an entitlement the reviewed file does not list"
# code added after signing, with no signature of its own
mkdir -p "$WORK/late/Werkbord.app" && ditto "$WORK/good/Werkbord.app" "$WORK/late/Werkbord.app"
cp /usr/bin/true "$WORK/late/Werkbord.app/Contents/Resources/helper-added-later"
out=$(check --distribution "$WORK/late/Werkbord.app" 2>&1) && bad "code added after signing was accepted" "$out"
ok "--distribution rejects code added after signing"
# a real ad hoc bundle is a build for this computer, not for distribution
fakeapp "$WORK/adhoc"
/usr/bin/codesign --force --sign - "$WORK/adhoc/Werkbord.app/Contents/Helpers/werkbord"; /usr/bin/codesign --force --sign - "$WORK/adhoc/Werkbord.app"
scripts/check-desktop-signature.sh --adhoc "$WORK/adhoc/Werkbord.app" >/dev/null || bad "an ad hoc bundle must pass --adhoc"
expect_fail "an ad hoc bundle is not a distribution" "ad hoc" scripts/check-desktop-signature.sh --distribution "$WORK/adhoc/Werkbord.app"
ok "--adhoc accepts what a local build makes"

# =================================================================== 3. a release refuses anything unsigned
echo "3. --release refuses what must not be published"
[ -f internal/webui/dist/index.html ] || bad "the web app is not built: run 'make web web-embed' first"
cur="v$(scripts/product.sh werkbord version)"
rel() { env -u CODESIGN_IDENTITY -u CODESIGN_TIMESTAMP -u CODESIGN -u CODESIGN_KEYCHAIN -u RELEASE OUT="$WORK/rel-out" "$@"; }
expect_fail "a release without an identity" "never ad hoc" rel VERSION=$cur scripts/build-desktop.sh --release
expect_fail "a release with an ad hoc identity" "never ad hoc" rel VERSION=$cur CODESIGN_IDENTITY=- scripts/build-desktop.sh --release
expect_fail "a release by RELEASE=1 is the same" "never ad hoc" rel VERSION=$cur RELEASE=1 scripts/build-desktop.sh --package
expect_fail "a release cannot skip the timestamp" "tests only" rel VERSION=$cur CODESIGN_IDENTITY="$ID" CODESIGN_TIMESTAMP=none scripts/build-desktop.sh --release
expect_fail "a release cannot use another codesign" "tests only" rel VERSION=$cur CODESIGN_IDENTITY="$ID" CODESIGN="$SHIM" scripts/build-desktop.sh --release
expect_fail "a release is exactly a version" "exactly a release" rel VERSION=v1.2.3-4-gabcdef CODESIGN_IDENTITY="$ID" scripts/build-desktop.sh --release
expect_fail "a release is the version in the VERSION file" "cmd/werkbord/VERSION" rel VERSION=v9.9.9 CODESIGN_IDENTITY="$ID" scripts/build-desktop.sh --release
expect_fail "a release carries the updater" "carries Sparkle" rel VERSION=$cur CODESIGN_IDENTITY="$ID" SPARKLE=0 scripts/build-desktop.sh --release
expect_fail "a release cannot name its own update key" "SPARKLE_PUBLIC_KEY is for tests only" rel VERSION=$cur CODESIGN_IDENTITY="$ID" SPARKLE_PUBLIC_KEY=c2hvcnQ= scripts/build-desktop.sh --release
expect_fail "a release cannot name its own update feed" "SPARKLE_FEED_URL is for tests only" rel VERSION=$cur CODESIGN_IDENTITY="$ID" SPARKLE_FEED_URL=http://127.0.0.1:1/appcast.xml scripts/build-desktop.sh --release
expect_fail "a release is never the build that installs updates without asking" "UPDATER_TEST is for tests only" rel VERSION=$cur CODESIGN_IDENTITY="$ID" UPDATER_TEST=1 scripts/build-desktop.sh --release
# The notarization check comes after the one that says a release is exactly a version, and a version that is a prerelease
# (cmd/werkbord/VERSION says 1.3.1-preview.2, say) never gets that far. So this one is tried on a copy of the scripts whose
# VERSION is a release, which refuses before it builds anything and leaves this tree alone.
RELTREE="$WORK/reltree"
mkdir -p "$RELTREE/cmd/werkbord" "$RELTREE/internal/webui"
cp -R scripts "$RELTREE/"
echo 1.2.3 > "$RELTREE/cmd/werkbord/VERSION"
ln -s "$PWD/internal/webui/dist" "$RELTREE/internal/webui/dist"
[ ! -d desktop ] || ln -s "$PWD/desktop" "$RELTREE/desktop"
in_reltree() { (cd "$RELTREE" && "$@"); }
expect_fail "a release must be able to notarize" "NOTARY_KEY_FILE, NOTARY_KEY_ID and NOTARY_ISSUER" in_reltree rel VERSION=v1.2.3 CODESIGN_IDENTITY="$ID" scripts/build-desktop.sh --release
expect_fail "--notarize needs a real identity" "Developer ID identity" scripts/build-desktop.sh --notarize
expect_fail "a release cannot use other notarization programs" "tests only" rel VERSION=$cur CODESIGN_IDENTITY="$ID" XCRUN=/bin/true NOTARY_KEY_FILE=x NOTARY_KEY_ID=x NOTARY_ISSUER=x scripts/build-desktop.sh --release
expect_fail "a timestamp setting must be 'none'" "can only be" env CODESIGN_TIMESTAMP=whenever scripts/build-desktop.sh
[ ! -e "$WORK/rel-out/Werkbord.app" ] || bad "a refused release built something"
ok "a refused release builds nothing"

if [ -n "$FAST" ]; then
  echo "(--fast: not building the app)"
  echo "ok: $pass checks"
  exit 0
fi

# =================================================================== 4. the real build, signed through the recorder
echo "4. the real build"
cur_ver=$(scripts/product.sh werkbord build-version)
OUTDIR="$WORK/out"
LOG="$SHIM_DIR/log"; : > "$LOG"
# Apple's tools are the fakes of scripts/test-support, writing to the same log, so that the order of everything shows in one place.
printf '#!/bin/sh\necho "HDIUTIL $1" >> "$SHIM_DIR/log"\nexec /usr/bin/hdiutil "$@"\n' > "$WORK/hdiutil"
chmod +x "$WORK/hdiutil"
export FAKE_LOG="$LOG" FAKE_COUNT="$WORK/count" FAKE_MODE=accepted FAKE_KEY="$WORK/AuthKey_TEST.p8"
echo "not a real key" > "$FAKE_KEY"
ARCH=universal CODESIGN="$SHIM" HDIUTIL="$WORK/hdiutil" CODESIGN_IDENTITY="$ID" CODESIGN_TIMESTAMP=none CODESIGN_KEYCHAIN="$WORK/some.keychain-db" \
  XCRUN="$ROOT/scripts/test-support/fake-xcrun.sh" SPCTL="$ROOT/scripts/test-support/fake-spctl.sh" NOTARY_KEY_FILE="$FAKE_KEY" NOTARY_KEY_ID=TESTKEYID NOTARY_ISSUER=TESTISSUER NOTARY_RETRY_SLEEP=0 \
  scripts/build-desktop.sh --package --notarize "$OUTDIR" >"$WORK/build.log" 2>&1 || { cat "$WORK/build.log" >&2; bad "the build with an identity failed"; }
contains "$(cat "$WORK/build.log")" "TEST ONLY" || bad "a build without a timestamp must say it is for tests only"
# one word per step of the build, in the order they happened (the image's name varies with the version)
steps=$(awk '
  /^SIGN /      { n = split($2, p, "/"); name = p[n]; if (name ~ /\.dmg$/) name = "the-image"; print "sign:" name; next }
  /^HDIUTIL /   { print "hdiutil:" $2; next }
  /^xcrun /     { print "xcrun:" $2 ":" $3; next }
  /^spctl /     { print "spctl:" $3 ":" $4; next }
' "$LOG" | tr '\n' ' ')
want="sign:werkbord sign:Werkbord.app xcrun:notarytool:submit xcrun:stapler:staple xcrun:stapler:validate spctl:--type:execute hdiutil:create sign:the-image xcrun:notarytool:submit xcrun:stapler:staple xcrun:stapler:validate spctl:--type:open "
[ "$steps" = "$want" ] || bad "the build did things in the wrong order" "got:  $steps
want: $want"
ok "in order: sign program, sign app, notarize+staple app, make the image, sign image, notarize+staple image"
SIGNS=$(grep '^SIGN' "$LOG")
first=$(printf '%s\n' "$SIGNS" | sed -n 1p); second=$(printf '%s\n' "$SIGNS" | sed -n 2p); third=$(printf '%s\n' "$SIGNS" | sed -n 3p)
contains "$first" "Contents/Helpers/werkbord" || bad "the program in Helpers is signed first" "$SIGNS"
contains "$second" "Werkbord.app |" || bad "the app is signed second, after what it holds" "$SIGNS"
contains "$third" ".dmg" || bad "the disk image is signed last, after the app inside it" "$SIGNS"
ok "signed inside-out: program, then app, then disk image"
[ "$(printf '%s\n' "$SIGNS" | grep -c .)" = 3 ] || bad "every piece of code is signed once and by name (no --deep)" "$SIGNS"
contains "$first" "runtime=1" && contains "$second" "runtime=1" || bad "the hardened runtime is on for the code" "$SIGNS"
contains "$third" "runtime=0" || bad "a disk image is not code: no hardened runtime on it" "$SIGNS"
contains "$first$second$third" "identity=$ID" || bad "signed with the identity"
contains "$first" "keychain=$WORK/some.keychain-db" && contains "$third" "keychain=$WORK/some.keychain-db" || bad "codesign is told which keychain holds the identity" "$SIGNS"
contains "$second" "entitlements=desktop/build/darwin/entitlements.plist" || bad "the app carries the reviewed entitlements and the program none" "$SIGNS"
ok "hardened runtime, the identity, its keychain and the reviewed entitlements, on what they belong on"
dmgfile=$(ls "$OUTDIR"/*.dmg)
case "$dmgfile" in *_darwin_universal.dmg) ;; *) bad "the image is named for its chip, not universal: $dmgfile" ;; esac
ok "the disk image is Werkbord_<version>_darwin_universal.dmg"
(cd "$OUTDIR" && shasum -a 256 -c "$(basename "$dmgfile").sha256" >/dev/null) || bad "the checksum is not of the finished disk image"
ok "the published checksum is of the finished image"

APP="$OUTDIR/Werkbord.app"
for exe in "$APP/Contents/MacOS/Werkbord" "$APP/Contents/Helpers/werkbord"; do
  archs=$(lipo -archs "$exe")
  [ "$archs" = "x86_64 arm64" ] || [ "$archs" = "arm64 x86_64" ] || bad "$exe is not one program for both kinds of Mac ($archs)"
done
# the two are not the same file under two names (the window, a few MB, and the program, tens of MB, are different programs)
[ "$(lipo -archs "$APP/Contents/MacOS/Werkbord" | wc -w | tr -d ' ')" = 2 ] && ! cmp -s "$APP/Contents/MacOS/Werkbord" "$APP/Contents/Helpers/werkbord" || bad "the window and the program are the same file"
ok "the window and the program are each one universal program: Apple Silicon and Intel in one app"

# =================================================================== 4b. the Team service the app carries
echo "4b. the Team service in the app"
: > "$LOG"
OUT2="$WORK/out-team"
TEAM_PAYLOAD=1 ARCH=universal CODESIGN="$SHIM" CODESIGN_IDENTITY="$ID" CODESIGN_TIMESTAMP=none CODESIGN_KEYCHAIN="$WORK/some.keychain-db" \
  scripts/build-desktop.sh "$OUT2" >"$WORK/build2.log" 2>&1 || { cat "$WORK/build2.log" >&2; bad "the build that carries Team's service failed"; }
signed=$(grep '^SIGN' "$LOG" | awk '{ n = split($2, p, "/"); print p[n] }' | tr '\n' ' ')
# rqlited first and once: the Team service is built with the hash of exactly those bytes, so signing it again would make another
# file. Nebula is never signed: it keeps its makers' signature and is held to its pin.
[ "$signed" = "rqlited werkbord werkbord-team Werkbord.app " ] || bad "the Team service's pieces are not signed as they must be" "got: $signed"
ok "signed: the database program first and once, then the programs, then the app; the network program never"
T="$OUT2/Werkbord.app"
want=$(sed -n 's/.*"sha256":"\([a-f0-9]*\)".*/\1/p' "$T/Contents/Resources/rqlited.build")
[ "$(shasum -a 256 "$T/Contents/Helpers/rqlited" | cut -d' ' -f1)" = "$want" ] || bad "the database program is not the one the build record names"
grep -aq "$want" "$T/Contents/Helpers/werkbord-team" || bad "the Team service was not built with the hash of the database program it ships with"
ok "the Team service carries the hash of the database program it ships with"
for exe in "$T/Contents/Helpers/werkbord-team" "$T/Contents/Helpers/rqlited"; do
  archs=$(lipo -archs "$exe")
  [ "$archs" = "x86_64 arm64" ] || [ "$archs" = "arm64 x86_64" ] || bad "$exe is not universal ($archs)"
done
CODESIGN="$SHIM" scripts/check-team-payload.sh --distribution "$T" >/dev/null || bad "check-team-payload refuses the Team service in a build it should accept"
ok "the Team service is universal and passes its own check"
/usr/bin/codesign --verify --strict --deep "$APP" || bad "the app does not verify"
/usr/bin/codesign -dvv "$APP" 2>&1 | grep -q 'runtime' || bad "the real signature has no hardened runtime flag" "$(/usr/bin/codesign -dvv "$APP" 2>&1)"
/usr/bin/codesign -d --entitlements - --xml "$APP" 2>/dev/null | grep -q 'com.apple' && bad "the app asks for an entitlement"
ok "the signed app verifies (strictly, deeply), is hardened, and holds no entitlement"
expected="v$(scripts/product.sh werkbord version)"
reported=$("$APP/Contents/Helpers/werkbord" version)
case "$cur_ver" in "$expected") [ "$reported" = "$expected" ] || bad "the signed program says $reported" ;; *) contains "$reported" "$expected" || bad "the signed program says $reported" ;; esac
ok "the signed, hardened program runs: werkbord version says $reported"

# =================================================================== 5. what is signed runs
echo "5. the hardened program and app run"
# 5a. a controller, started by the hardened program, in a home of its own
H="$WORK/home-run"; mkdir -p "$H"
PORT=$(free_port)
envrun() { HOME="$H" WERKBORD_DATA_DIR="$H/data" DEVBOARD_DATA_DIR="" WERKBORD_ADDR="127.0.0.1:$PORT" DEVBOARD_ADDR="" "$@"; }
envrun "$APP/Contents/Helpers/werkbord" setup --no-service --no-open --no-network >"$WORK/setup.log" 2>&1 || { cat "$WORK/setup.log" >&2; bad "the hardened program could not set Werkbord up"; }
code=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$PORT/api/health" || true)
[ "$code" = 200 ] || bad "the controller started by the hardened program does not answer (HTTP $code)" "$(cat "$WORK/setup.log")"
health=$(curl -s "http://127.0.0.1:$PORT/api/health")
contains "$health" "$reported" || bad "the controller reports another version" "$health"
tok=$(cat "$H/data/token")
code=$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $tok" "http://127.0.0.1:$PORT/api/projects" || true)
[ "$code" = 200 ] || bad "the hardened controller does not accept its own token"
code=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$PORT/api/projects" || true)
[ "$code" = 401 ] || bad "the hardened controller answers without a token (HTTP $code)"
ok "a controller started by the hardened program answers, and still demands its token"
envrun "$APP/Contents/Helpers/werkbord" stop >/dev/null 2>&1 || true

# 5b. the launcher's end-to-end scenarios, on hardened binaries
echo "   launcher scenarios on hardened binaries (this builds and runs the real program several times)"
WERKBORD_E2E_HARDENED=1 go test ./internal/launcher -run 'TestTheDesktopLifecycleWithTheRealProgram' -count=1 >"$WORK/e2e.log" 2>&1 || { cat "$WORK/e2e.log" >&2; bad "the launcher scenarios fail on hardened binaries"; }
ok "the launcher's end-to-end scenarios pass on hardened-runtime binaries (install, start, update, rollback rules)"

# 5c. the embedded Tailscale node, hardened
go test -c -o "$WORK/netprivate.test" ./internal/netprivate >/dev/null 2>&1 || bad "could not build the private network tests"
/usr/bin/codesign --force --options runtime --sign - "$WORK/netprivate.test"
/usr/bin/codesign -dvv "$WORK/netprivate.test" 2>&1 | grep -q runtime || bad "the test binary is not hardened"
( cd "$ROOT/internal/netprivate" && "$WORK/netprivate.test" -test.run 'TestRealNode' -test.count=1 -test.timeout=4m ) >"$WORK/tailscale.log" 2>&1 || { tail -30 "$WORK/tailscale.log" >&2; bad "the embedded Tailscale node does not run under the hardened runtime"; }
ok "the embedded Tailscale node starts, signs in, serves and keeps its identity under the hardened runtime, with no entitlement"

# 5d. the signed app: opens a window and connects, as the Mac it was built on runs it, and (Rosetta, if there is any) as an Intel Mac does
run_apps  # (defined above, before part 1, so that APP_ONLY=… can run just this)

# the installed Werkbord was never involved
[ "$RUNNING_BEFORE" = "$(running)" ] || bad "a controller on this computer started, stopped or restarted during the test" "before:
$RUNNING_BEFORE
after:
$(running)"
ok "the controller that was running on this computer is untouched"

echo "ok: $pass checks"
