#!/bin/sh
# Tests the app updating ITSELF, with the real Sparkle, two real builds of the app (1.2.0 and 1.2.1), and a feed on this
# computer. Nothing here is a fake of the updater: what is substituted is the person (the test build installs a valid update
# without asking, desktop/updater_darwin.m) and the internet (a local web server).
#
#   scripts/test-desktop-update.sh                  everything
#   ONLY="good tampered" scripts/test-desktop-update.sh      some scenarios (good tampered wrongsig wrongkeyfeed unsignedfeed
#                                                             downgrade replay off deferred menu)
#
# Scenarios, and what each must show:
#   good         1.2.0 finds 1.2.1 in a valid, signed feed, verifies it, installs it, quits, and comes back as 1.2.1; the
#                controller keeps its token and its data, is brought onto the new program ONCE (after the app update, by the
#                installer, with a snapshot), and answers with its token; the app made no second window or process.
#   tampered     the archive was changed after it was signed: refused, nothing installed, same app, same controller.
#   wrongsig     the feed is right but the archive is signed with another key: refused.
#   wrongkeyfeed the feed is signed with another key: refused. unsignedfeed: the feed is not signed at all: refused.
#   downgrade    the feed offers an older version: nothing to install.
#   replay       the feed claims a newer version but the archive is an older, validly signed app: refused as a downgrade.
#   off          noUpdateCheck: not one request reaches the feed.
#   deferred     coding agents are working when the app is updated: the app replacement waits; the controller keeps running on
#                its old program with the SAME process, is told once why, and moves to the new program when the work is done.
#   menu         the application menu has one "Check for Updates…", under About, and Help has none.
#
# Safety. The user's own Werkbord is on this computer. A test build can only run in a throwaway home directory: it takes its
# environment from a file (because the system relaunches it with none), has its own bundle identifier, and exits at start
# if its home is the real one (desktop/updater_testenv_darwin.go). And this script checks, before and after every scenario,
# that the real controller, its program, its login service and its data directory are exactly as they were.
#
# What this cannot show: the update window, which is Sparkle's own and which no test here clicks (the test build skips
# it); and an update signed with a real Developer ID and notarized, which needs the real certificate (the first release,
# docs/DESKTOP_RELEASE.md). The signature check Sparkle makes of the new app's code is made here with ad hoc signatures.
set -eu
# Isolated Individual signature/updater fixtures. Unified payloads are tested by
# test-unified-installer.sh; production --release rejects this override.
export TEAM_PAYLOAD=0

cd "$(dirname "$0")/.."
ROOT=$(pwd)
[ "$(uname -s)" = Darwin ] || { echo "test-desktop-update: needs macOS"; exit 0; }
[ -f internal/webui/dist/index.html ] || { echo "the web app is not built: run 'make web web-embed' first" >&2; exit 1; }
command -v python3 >/dev/null || { echo "python3 is needed" >&2; exit 1; }

# UPDATE_TEST_DIR=<dir> keeps the keys, the port and the two builds there, so that a run after a change to the test (not to the
# app) does not build them again. Without it everything is made new and removed.
if [ -n "${UPDATE_TEST_DIR:-}" ]; then WORK=$UPDATE_TEST_DIR; mkdir -p "$WORK"; KEEP=1; else WORK=$(mktemp -d); KEEP=""; fi
FEEDPID=""
cleanup() {
  forget_the_test_app 2>/dev/null || true
  [ -z "$FEEDPID" ] || kill "$FEEDPID" 2>/dev/null || true
  # whatever this test started: the apps and controllers under $WORK, and nothing else
  for pid in $(pgrep -f "$WORK" 2>/dev/null || true); do
    case "$pid" in "$$") ;; *) kill "$pid" 2>/dev/null || true ;; esac
  done
  if [ -z "$KEEP" ]; then rm -rf "$WORK"; else rm -rf "$WORK"/home-* "$WORK/feeds"; fi
}
trap cleanup EXIT INT TERM

pass=0
ok() { pass=$((pass + 1)); printf '  ok  %s\n' "$1"; }
bad() { printf 'FAIL  %s\n' "$1" >&2; [ -n "${2:-}" ] && printf '%s\n' "$2" >&2; exit 1; }
contains() { case "$1" in *"$2"*) return 0 ;; esac; return 1; }
wanted() { [ -z "${ONLY:-}" ] || contains " $ONLY " " $1 "; }
free_port() { python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])'; }

# ------------------------------------------------------------------ the real installation, which nothing here may touch
REAL_HOME=$(python3 -c 'import pwd,os; print(pwd.getpwuid(os.getuid()).pw_dir)')
real_state() {
  {
    ps -axo pid=,lstart=,command= | grep '[w]erkbord serve' | grep -v "$WORK" || true
    for f in "$REAL_HOME/.local/bin/werkbord" "$REAL_HOME/.local/bin/devboard"; do [ -e "$f" ] && { ls -l "$f"; shasum "$f"; } || echo "no $f"; done
    ls -l "$REAL_HOME/Library/LaunchAgents" 2>/dev/null | grep -i -E 'werkbord|devboard' || true
    # (not the test app's own entries: LaunchServices lists a running app, and the test app is dev.werkbord.desktop.test)
    launchctl list 2>/dev/null | grep -i -E 'werkbord|devboard' | grep -v 'dev\.werkbord\.desktop\.test' || true
    ls -ld "$REAL_HOME/Library/Application Support/werkbord" "$REAL_HOME/Library/Application Support/devboard" 2>/dev/null || true
  } 2>&1
}
REAL_BEFORE=$(real_state)
tripwire() {
  now=$(real_state)
  [ "$now" = "$REAL_BEFORE" ] || bad "the installation of the person running this test changed ($1)" "before:
$REAL_BEFORE
now:
$now"
}
# macOS keeps an app's preferences under the real home whatever HOME says, so a test app's are written there, under ITS OWN
# identifier (dev.werkbord.desktop.test, which is why it has one). They are removed here, and the real app's are checked untouched.
REAL_PREFS="$REAL_HOME/Library/Preferences/dev.werkbord.desktop.plist"
REAL_PREFS_BEFORE=$(shasum "$REAL_PREFS" 2>/dev/null || echo none)
forget_the_test_app() {
  defaults delete dev.werkbord.desktop.test >/dev/null 2>&1 || true
  rm -f "$REAL_HOME/Library/Preferences/dev.werkbord.desktop.test.plist"
  rm -rf "$REAL_HOME/Library/Caches/dev.werkbord.desktop.test" "$REAL_HOME/Library/HTTPStorages/dev.werkbord.desktop.test" "$REAL_HOME/Library/WebKit/dev.werkbord.desktop.test"
}
no_real_state_for_the_test_app() {
  [ "$(shasum "$REAL_PREFS" 2>/dev/null || echo none)" = "$REAL_PREFS_BEFORE" ] || bad "a test app changed the real app's preferences ($REAL_PREFS)"
  forget_the_test_app
}
forget_the_test_app

# ------------------------------------------------------------------ the pieces
SPARKLE_HOME=$(scripts/fetch-sparkle.sh)
go build -o "$WORK/appcast" ./scripts/appcast
export SPARKLE_DIR="$SPARKLE_HOME"
[ -f "$WORK/keyA" ] || "$WORK/appcast" keygen "$WORK/keyA" > "$WORK/keyA.pub"      # the app's key
[ -f "$WORK/keyB" ] || "$WORK/appcast" keygen "$WORK/keyB" > "$WORK/keyB.pub"      # someone else's
PUBA=$(cat "$WORK/keyA.pub")
[ -f "$WORK/feedport" ] || free_port > "$WORK/feedport"
FEEDPORT=$(cat "$WORK/feedport")
HOST=$(uname -m | sed 's/x86_64/amd64/')
export UPDATER_TEST=1 UPDATER_TEST_ENV_FILE="$WORK/env.json" SPARKLE_PUBLIC_KEY="$PUBA" SPARKLE_FEED_URL="http://127.0.0.1:$FEEDPORT/appcast.xml" ARCH=$HOST

for v in 1.2.0 1.2.1; do
  if [ -d "$WORK/build-$v/Werkbord.app" ]; then echo "(reusing the build of $v in $WORK)"; else
    echo "building $v, with Sparkle, as a test build"
    VERSION=v$v OUT="$WORK/build-$v" scripts/build-desktop.sh --package >"$WORK/build-$v.log" 2>&1 || { cat "$WORK/build-$v.log" >&2; bad "building $v failed"; }
  fi
  scripts/check-desktop-updater.sh "$WORK/build-$v/Werkbord.app" >/dev/null || bad "the updater settings of $v are wrong"
done
unset UPDATER_TEST UPDATER_TEST_ENV_FILE SPARKLE_PUBLIC_KEY SPARKLE_FEED_URL ARCH
ZIP120="$WORK/build-1.2.0/Werkbord_1.2.0_darwin_$HOST.zip"
ZIP121="$WORK/build-1.2.1/Werkbord_1.2.1_darwin_$HOST.zip"
ok "two builds of the app, each with the pinned Sparkle, its feed, its key, no schedule and no unattended install"

# ------------------------------------------------------------------ the feed: a web server on this computer
cat > "$WORK/feed.py" <<'PY'
import http.server, os, sys
port, root, log = int(sys.argv[1]), sys.argv[2], sys.argv[3]
class H(http.server.SimpleHTTPRequestHandler):
    def translate_path(self, p):
        return os.path.join(root, p.split("?")[0].lstrip("/"))
    def log_message(self, fmt, *a):
        with open(log, "a") as f:
            f.write("%s\n" % (fmt % a))
http.server.ThreadingHTTPServer(("127.0.0.1", port), H).serve_forever()
PY
mkdir -p "$WORK/feeds"; : > "$WORK/feed.log"
ln -sfn "$WORK/feeds" "$WORK/serve"
python3 "$WORK/feed.py" "$FEEDPORT" "$WORK/serve" "$WORK/feed.log" &
FEEDPID=$!

# make_feed <name> <zip> [key file]: a feed, in feeds/<name>, as a release publishes it
make_feed() {
  d="$WORK/feeds/$1"; mkdir -p "$d"
  SPARKLE_ED_KEY_FILE=${3:-$WORK/keyA} DOWNLOAD_URL_PREFIX="http://127.0.0.1:$FEEDPORT/" scripts/make-appcast.sh "$2" "$d" >"$WORK/make-$1.log" 2>&1 || { cat "$WORK/make-$1.log" >&2; bad "could not make the feed $1"; }
  cp "$2" "$d/"
}
use_feed() { ln -sfn "$WORK/feeds/$1" "$WORK/serve"; : > "$WORK/feed.log"; }

# ------------------------------------------------------------------ a computer for one scenario
# scenario <name> <build version>: a home, data and port of its own, the app installed in <home>/Applications, and
# the environment file the test app reads (it is the same file for the app that comes back after an update)
scenario() {
  S=$1; V=$2
  H="$WORK/home-$S"; mkdir -p "$H/Applications"
  DATA="$H/data"; ADDR="127.0.0.1:$(free_port)"
  LOG="$H/Library/Logs/Werkbord/desktop.log"
  APPDIR="$H/Applications/Werkbord.app"
  extra=${3:-}
  printf '{"HOME":"%s","WERKBORD_DATA_DIR":"%s","WERKBORD_ADDR":"%s","WERKBORD_DESKTOP_NO_SERVICE":"1","WERKBORD_UPDATER_TEST":"1","WERKBORD_UPDATER_TEST_TRIGGER":"%s/updater-go"%s}\n' "$H" "$DATA" "$ADDR" "$H" "$extra" > "$WORK/env.json"
  ditto "$WORK/build-$V/Werkbord.app" "$APPDIR"
  tripwire "before $S"
}
ask_for_a_check() { : > "$H/updater-go"; }   # the test build checks once, when this file exists
launch() {
  "$APPDIR/Contents/MacOS/Werkbord" >"$H/app.out" 2>&1 &
  FIRST=$!
}
logged() { grep -q "$1" "$LOG" 2>/dev/null; }
wait_log() { # <pattern> <seconds> <what>
  n=0
  while ! logged "$1"; do
    n=$((n + 1))
    [ $n -lt $(($2 * 2)) ] || bad "$S: $3 (never saw \"$1\" in the app's log)" "$(tail -30 "$LOG" 2>/dev/null)
$(cat "$H/app.out" 2>/dev/null)"
    sleep 0.5
  done
}
bundle_version() { plutil -extract CFBundleShortVersionString raw -o - "$APPDIR/Contents/Info.plist"; }
app_pids() { pgrep -f "$APPDIR/Contents/MacOS/Werkbord" || true; }
ctl_pid() { cat "$DATA/controller.pid" 2>/dev/null || echo 0; }
token() { cat "$DATA/token"; }
api() { curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $(token)" "http://$ADDR$1"; }
health() { curl -s "http://$ADDR/api/health"; }
installed_version() { "$H/.local/bin/werkbord" version; }
# quit the app and wait until it has really gone (a second launch while the first is still going finds its single-instance lock and quits)
quit_app() {
  for p in $(app_pids); do kill "$p" 2>/dev/null || true; done
  n=0; while [ -n "$(app_pids)" ]; do n=$((n + 1)); [ $n -lt 120 ] || { for p in $(app_pids); do kill -9 "$p" 2>/dev/null || true; done; break; }; sleep 0.5; done
  sleep 1
}
# until the controller is on <version>: the app brings it there when it opens, and if the installer cannot do it then (on a computer this busy
# even its look at the controller can run out of time, which it answers safely, by refusing), the app is opened again, as a person would,
# up to three times.
await_controller_on() { # <version without v>
  opening=0
  while :; do
    opening=$((opening + 1))
    n=0
    until { [ "$(installed_version)" = "v$1" ] && case "$(health)" in *"v$1"*) true ;; *) false ;; esac; } || [ $n -ge 240 ]; do n=$((n + 1)); sleep 0.5; done
    [ "$(installed_version)" = "v$1" ] && return 0
    [ $opening -lt 3 ] || bad "$S: the controller did not come up on $1 in $opening openings" "$(tail -20 "$LOG")"
    echo "  (opening $opening did not complete the move: $(grep 'msg=notice' "$LOG" | tail -1 | sed 's/.*still on [0-9.]*: //' | cut -c1-110)...; opening again, as a person would)"
    quit_app
    launch
  done
}
stop_all() {
  for p in $(app_pids); do kill "$p" 2>/dev/null || true; done
  [ -x "$H/.local/bin/werkbord" ] && HOME="$H" WERKBORD_DATA_DIR="$DATA" WERKBORD_ADDR="$ADDR" "$H/.local/bin/werkbord" stop >/dev/null 2>&1 || true
  sleep 1
}
first_run() { # the app installs its program, sets Werkbord up without a login service, starts the controller, connects
  launch
  wait_log "msg=connected" 90 "the app did not connect on its first run"
  [ "$(installed_version)" = "v$V" ] || bad "$S: the program installed by the app is $(installed_version), not v$V"
  case "$(health)" in *"v$V"*) ;; *) bad "$S: the controller does not say v$V: $(health)" ;; esac
}

# ===================================================================== good
if wanted good; then
  echo "good: a valid update is installed, and the app comes back as 1.2.1"
  make_feed good "$ZIP121"
  use_feed good
  scenario good 1.2.0
  launch
  wait_log "msg=connected" 90 "no first connection"
  P0=$(ctl_pid); TOK=$(token)
  [ "$(installed_version)" = "v1.2.0" ] || bad "good: first run installed $(installed_version)"
  wait_log "the app can update itself" 10 "Sparkle did not start in the test app"
  ask_for_a_check
  wait_log "found an update" 60 "the update was not found"
  wait_log "starting to unpack the update" 60 "the update was not unpacked"
  wait_log "installing the update" 60 "the update was not installed"
  ok "good: found, downloaded, unpacked (Sparkle checks the signature first), and installing"
  n=0; until [ "$(bundle_version)" = "1.2.1" ] && logged 'msg=starting version=v1.2.1'; do n=$((n + 1)); [ $n -lt 120 ] || bad "good: the app did not come back as 1.2.1" "$(tail -20 "$LOG")"; sleep 0.5; done
  ok "good: the app on disk is 1.2.1 and a process of it started (log: starting version=v1.2.1)"
  /usr/bin/codesign --verify --deep --strict "$APPDIR" || bad "good: the updated app's signature is not valid"
  kill -0 "$FIRST" 2>/dev/null && bad "good: the old process is still running"
  [ "$(app_pids | wc -l | tr -d ' ')" = 1 ] || bad "good: there is not exactly one app process: $(app_pids)"
  ok "good: exactly one process, the new one; the bundle's signature verifies"
  wait_log "phase=upgrading" 60 "the new app did not bring the controller up to date"
  await_controller_on 1.2.1
  P1=$(ctl_pid)
  [ "$P1" != "$P0" ] && kill -0 "$P1" 2>/dev/null || bad "good: the controller was not restarted exactly once on the new program ($P0 → $P1)"
  [ "$(token)" = "$TOK" ] && [ "$(api /api/projects)" = 200 ] || bad "good: the token or the data did not survive"
  [ -n "$(ls "$DATA"/backups/before-update-*.db 2>/dev/null)" ] || bad "good: no database snapshot was taken before the controller moved"
  # Health answers before the installer has completed authenticated database
  # verification. Await that final transaction before asserting backup cleanup.
  n=0; while [ -e "$H/.local/bin/werkbord.prev" ]; do n=$((n+1)); [ $n -lt 60 ] || bad "good: the old program was left behind"; sleep 0.5; done
  ok "good: the new app moved the controller to its program once (new process, snapshot taken, token and data intact, no leftover)"
  case "$(api /api/projects)$(curl -s -o /dev/null -w '%{http_code}' "http://$ADDR/api/projects")" in 200401) ok "good: and authentication is as strict as ever" ;; *) bad "good: authentication changed" ;; esac
  stop_all
  tripwire "after good"
fi

# a feed with one thing wrong, and a scenario that must refuse it
refuses() { # <name> <why it is wrong, for the report>
  scenario "$1" 1.2.0
  use_feed "$1"
  first_run
  P0=$(ctl_pid)
  wait_log "the app can update itself" 10 "Sparkle did not start"
  ask_for_a_check
  wait_log "refused or could not finish an update" 60 "the bad update was not refused"
  sleep 4
  logged "installing the update" && bad "$1: an update that is $2 was installed"
  logged "relaunching" && bad "$1: the app relaunched"
  [ "$(bundle_version)" = 1.2.0 ] && kill -0 "$FIRST" 2>/dev/null || bad "$1: the app changed or went away"
  [ "$(ctl_pid)" = "$P0" ] && kill -0 "$P0" 2>/dev/null && [ "$(installed_version)" = v1.2.0 ] || bad "$1: the controller or its program changed"
  ok "$1: an update that is $2 is refused: nothing installed, same app, same process, same controller"
  stop_all
  tripwire "after $1"
}

if wanted tampered; then
  echo "tampered: changed after it was signed"
  make_feed tampered "$ZIP121"
  # one byte of the archive the server will serve is not what was signed (same length: only the signature can tell)
  python3 - "$WORK/feeds/tampered/$(basename "$ZIP121")" <<'PY'
import sys
p = sys.argv[1]
b = bytearray(open(p, "rb").read())
b[len(b) // 2] ^= 0xFF
open(p, "wb").write(b)
PY
  refuses tampered "altered after it was signed"
fi

# feeds that need editing: the appcast's text is changed and the feed signed again with a key
resign() { # <feed dir> <key>: sign the edited appcast.xml in that directory as Sparkle does
  python3 - "$1/appcast.xml" <<'PY'
import re, sys
p = sys.argv[1]
t = open(p).read()
t = re.sub(r"\n?<!-- sparkle-signatures:.*?-->\s*", "\n", t, flags=re.S)
open(p, "w").write(t)
PY
  "$SPARKLE_HOME/bin/sign_update" --ed-key-file "$2" "$1/appcast.xml" >/dev/null 2>&1 || bad "could not sign $1/appcast.xml"
}

if wanted wrongsig; then
  echo "wrongsig: the archive is signed by someone else"
  make_feed wrongsig "$ZIP121"
  SIGB=$("$WORK/appcast" sign "$WORK/keyB" "$ZIP121")
  python3 - "$WORK/feeds/wrongsig/appcast.xml" "$SIGB" <<'PY'
import re, sys
p, sig = sys.argv[1], sys.argv[2]
t = open(p).read()
t = re.sub(r'sparkle:edSignature="[^"]*"', 'sparkle:edSignature="%s"' % sig, t, count=1)
open(p, "w").write(t)
PY
  resign "$WORK/feeds/wrongsig" "$WORK/keyA" # the feed is genuine; the archive's signature is not
  refuses wrongsig "signed with a key the app does not trust"
fi

if wanted wrongkeyfeed; then
  echo "wrongkeyfeed: the feed itself is signed by someone else"
  make_feed wrongkeyfeed "$ZIP121"
  resign "$WORK/feeds/wrongkeyfeed" "$WORK/keyB"
  refuses wrongkeyfeed "in a feed signed with a key the app does not trust"
fi

if wanted unsignedfeed; then
  echo "unsignedfeed: the feed is not signed at all"
  make_feed unsignedfeed "$ZIP121"
  python3 - "$WORK/feeds/unsignedfeed/appcast.xml" <<'PY'
import re, sys
p = sys.argv[1]
t = open(p).read()
t = re.sub(r"\n?<!-- sparkle-signatures:.*?-->\s*", "\n", t, flags=re.S)
open(p, "w").write(t)
PY
  refuses unsignedfeed "in a feed that is not signed"
fi

# ===================================================================== downgrade, and replay of an old archive
if wanted downgrade; then
  echo "downgrade: the feed offers an older version than the app has"
  make_feed downgrade "$ZIP120"
  use_feed downgrade
  scenario downgrade 1.2.1
  first_run
  P0=$(ctl_pid)
  wait_log "the app can update itself" 10 "Sparkle did not start"
  ask_for_a_check
  n=0; until grep -q 'appcast.xml' "$WORK/feed.log"; do n=$((n + 1)); [ $n -lt 80 ] || bad "downgrade: the app never asked the feed"; sleep 0.5; done
  sleep 5
  logged "found an update" && bad "downgrade: an older version was offered as an update"
  [ "$(bundle_version)" = 1.2.1 ] && kill -0 "$FIRST" 2>/dev/null && [ "$(ctl_pid)" = "$P0" ] || bad "downgrade: something changed"
  ok "downgrade: 1.2.1 asked the feed, which had only 1.2.0, and found nothing to install"
  stop_all; tripwire "after downgrade"
fi

if wanted replay; then
  echo "replay: the feed says 9.9.9, but what is behind it is the old 1.2.0, validly signed"
  make_feed replay "$ZIP120"
  python3 - "$WORK/feeds/replay/appcast.xml" <<'PY'
import re, sys
p = sys.argv[1]
t = open(p).read()
t = t.replace("<sparkle:version>1.2.0</sparkle:version>", "<sparkle:version>9.9.9</sparkle:version>")
t = t.replace("<sparkle:shortVersionString>1.2.0</sparkle:shortVersionString>", "<sparkle:shortVersionString>9.9.9</sparkle:shortVersionString>")
open(p, "w").write(t)
PY
  grep -q "9.9.9" "$WORK/feeds/replay/appcast.xml" || bad "replay: could not edit the feed"
  resign "$WORK/feeds/replay" "$WORK/keyA" # an attacker cannot do this part; this is the best case for them: the genuine key signed a feed that lies about the version
  scenario replay 1.2.1
  use_feed replay
  first_run
  P0=$(ctl_pid)
  wait_log "the app can update itself" 10 "Sparkle did not start"
  ask_for_a_check
  wait_log "found an update" 60 "the feed's 9.9.9 was not even considered"
  wait_log "refused or could not finish an update" 90 "an old archive under a newer version number was not refused"
  sleep 4
  logged "relaunching" && bad "replay: the app relaunched"
  [ "$(bundle_version)" = 1.2.1 ] && kill -0 "$FIRST" 2>/dev/null && [ "$(ctl_pid)" = "$P0" ] || bad "replay: the app or controller changed"
  ok "replay: an old, validly signed archive offered under a newer version number was refused: the app stays 1.2.1"
  stop_all; tripwire "after replay"
fi

# ===================================================================== privacy
if wanted off; then
  echo "off: looking for updates is turned off"
  make_feed off "$ZIP121"
  use_feed off
  scenario off 1.2.0 ',"WERKBORD_NO_UPDATE_CHECK":"1"'
  first_run
  wait_log "the app can update itself" 10 "Sparkle did not start"
  ask_for_a_check
  sleep 12 # long enough for a check that was allowed to happen
  [ ! -s "$WORK/feed.log" ] || bad "off: the app asked the feed although looking for updates is turned off" "$(cat "$WORK/feed.log")"
  logged "found an update" && bad "off: an update was found"
  [ "$(bundle_version)" = 1.2.0 ] || bad "off: the app changed"
  logged "refused or could not finish an update" && ok "off: Sparkle itself refused the check ($(grep 'refused or could not' "$LOG" | head -1 | sed 's/.*text=//'))"
  ok "off: with noUpdateCheck the feed was not asked once, in 12 seconds, though a check was asked for, and the app is unchanged"
  stop_all; tripwire "after off"
fi

# ===================================================================== agents at work
if wanted deferred; then
  echo "deferred: coding agents are working while the app is updated"
  make_feed deferred "$ZIP121"
  use_feed deferred
  scenario deferred 1.2.0
  launch
  wait_log "msg=connected" 90 "no first connection"
  P0=$(ctl_pid)
  # a run that is not finished: what makes the installer refuse
  mkdir -p "$DATA/runner/runs"
  printf '{"phase":"running"}' > "$DATA/runner/runs/r1.json"
  wait_log "the app can update itself" 10 "Sparkle did not start"
  ask_for_a_check
  wait_log "waiting for the program's update to finish before replacing the app" 60 "active runner did not defer relaunch"
  sleep 3
  [ "$(bundle_version)" = "1.2.0" ] || bad "deferred: app replaced while runner work was active"
  [ "$(ctl_pid)" = "$P0" ] && kill -0 "$P0" 2>/dev/null || bad "deferred: active controller was interrupted"
  [ "$(installed_version)" = v1.2.0 ] || bad "deferred: backend changed under active work"
  ok "deferred: active runner prevents app replacement and backend replacement; original processes remain alive"
  rm -f "$DATA/runner/runs/r1.json"
  n=0; until [ "$(bundle_version)" = "1.2.1" ] && logged 'msg=starting version=v1.2.1'; do n=$((n + 1)); [ $n -lt 160 ] || bad "deferred: update did not resume after work ended" "$(tail -20 "$LOG")"; sleep 0.5; done
  opening=0
  while :; do
    opening=$((opening + 1))
    quit_app
    launch
    n=0
    until { [ "$(installed_version)" = v1.2.1 ] && case "$(health)" in *v1.2.1*) true ;; *) false ;; esac; } || [ $n -ge 240 ]; do n=$((n + 1)); sleep 0.5; done
    [ "$(installed_version)" = v1.2.1 ] && break
    [ $opening -lt 3 ] || bad "deferred: the controller did not move once the work was done, in $opening openings" "$(tail -20 "$LOG")"
    echo "  (opening $opening did not complete the move: $(grep 'msg=notice' "$LOG" | tail -1 | sed 's/.*still on 1.2.0: //' | cut -c1-110)...; opening again, as a person would)"
  done
  [ "$(ctl_pid)" != "$P0" ] || bad "deferred: same process after the move"
  ok "deferred: when the agents were done, the next opening moved the controller to the new program"
  stop_all; tripwire "after deferred"
fi

# ===================================================================== the menu
if wanted menu; then
  echo "menu: one 'Check for Updates…', in the application menu"
  make_feed menu "$ZIP121"
  use_feed menu
  scenario menu 1.2.0 ',"WERKBORD_NO_UPDATE_CHECK":"1"'
  first_run
  wait_log "the menu bar" 30 "the menu bar was not reported"
  m=$(grep 'the menu bar' "$LOG" | head -1)
  contains "$m" "Check for Updates…" || bad "menu: no Check for Updates… in the menus" "$m"
  [ "$(printf '%s' "$m" | grep -o 'Check for Updates…' | wc -l | tr -d ' ')" = 1 ] || bad "menu: more than one Check for Updates…" "$m"
  contains "$m" "About Werkbord; Check for Updates…;" || bad "menu: it is not directly under About" "$m"
  case "$m" in *"Help:"*"Check for Updates"*) bad "menu: Help has one too" "$m" ;; esac
  contains "$m" "Help:" && contains "$m" "Show Diagnostics" || bad "menu: Help is missing" "$m"
  ok "menu: exactly one Check for Updates…, directly under About Werkbord; Help has none"
  stop_all; tripwire "after menu"
fi

no_real_state_for_the_test_app
tripwire "at the end"
ok "the installation of the person running this test was never touched; the real app's own preferences are byte for byte as they were; the test app's were removed"
echo "ok: $pass checks"
