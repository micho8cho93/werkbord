#!/bin/sh
# Tests the installer against a release server on this computer: nothing is
# downloaded from the Internet and nothing outside a temporary directory is touched
# (no login service is installed: setup runs with --no-service).
#
#   scripts/test-install.sh
#
# Needs go, python3 and curl, and the web app built and embedded (make web web-embed).
set -eu

# The shell the installer is run with: it must work in a strict POSIX sh (dash on Debian and Ubuntu), not just bash.
SH=${SH:-sh}

cd "$(dirname "$0")/.."
ROOT=$(pwd)
WORK=$(mktemp -d)
SERVER_PID=""
cleanup() {
  [ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null || true
  # stop any controller a test left running
  for d in "$WORK"/home*/data; do
    [ -f "$d/controller.pid" ] && kill "$(cat "$d/controller.pid")" 2>/dev/null || true
  done
  rm -rf "$WORK"
}
trap cleanup EXIT INT TERM

pass=0
ok() { pass=$((pass + 1)); printf '  ok  %s\n' "$1"; }
bad() { printf 'FAIL  %s\n' "$1" >&2; [ -n "${2:-}" ] && printf '%s\n' "$2" >&2; exit 1; }
contains() { case "$1" in *"$2"*) return 0 ;; esac; return 1; }

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$(uname -m)" in x86_64|amd64) arch=amd64 ;; arm64|aarch64) arch=arm64 ;; *) echo "unsupported test machine"; exit 1 ;; esac

# ---- two releases, and a tampered one (named by their product tag, as GitHub has them) ----
mkdir -p "$WORK/releases"
for v in v9.0.1 v9.0.2; do
  PLATFORMS="$os/$arch" scripts/build-release.sh werkbord "$v" "$WORK/build-$v" >/dev/null
  mkdir -p "$WORK/releases/werkbord-$v"
  cp "$WORK/build-$v"/* "$WORK/releases/werkbord-$v/"
done
# v9.0.3: the archive is not what its checksum says (tampered in transit).
mkdir -p "$WORK/releases/werkbord-v9.0.3"
cp "$WORK/releases/werkbord-v9.0.2"/devboard_9.0.2_${os}_${arch}.tar.gz "$WORK/releases/werkbord-v9.0.3/devboard_9.0.3_${os}_${arch}.tar.gz"
printf '%s  devboard_9.0.3_%s_%s.tar.gz\n' "$(printf 'something else' | shasum -a 256 2>/dev/null | cut -d' ' -f1 || printf 'something else' | sha256sum | cut -d' ' -f1)" "$os" "$arch" > "$WORK/releases/werkbord-v9.0.3/checksums.txt"
# v9.0.4: a correctly checksummed archive whose executable is some other version.
mkdir -p "$WORK/releases/werkbord-v9.0.4"
cp "$WORK/releases/werkbord-v9.0.2"/devboard_9.0.2_${os}_${arch}.tar.gz "$WORK/releases/werkbord-v9.0.4/devboard_9.0.4_${os}_${arch}.tar.gz"
sum=$(shasum -a 256 "$WORK/releases/werkbord-v9.0.4/devboard_9.0.4_${os}_${arch}.tar.gz" 2>/dev/null || sha256sum "$WORK/releases/werkbord-v9.0.4/devboard_9.0.4_${os}_${arch}.tar.gz")
printf '%s  devboard_9.0.4_%s_%s.tar.gz\n' "$(printf '%s' "$sum" | cut -d' ' -f1)" "$os" "$arch" > "$WORK/releases/werkbord-v9.0.4/checksums.txt"
ok "built the test releases"

# ---- a releases page, laid out like GitHub's ----
cat > "$WORK/server.py" <<'PY'
import http.server, os, sys
root, latest = sys.argv[1], sys.argv[2]
class H(http.server.SimpleHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/latest":
            self.send_response(302); self.send_header("Location", "/tag/" + latest); self.end_headers(); return
        if self.path.startswith("/tag/"):
            self.send_response(200); self.end_headers(); self.wfile.write(b"release page"); return
        if self.path.startswith("/download/"):
            self.path = self.path[len("/download"):]
        return super().do_GET()
    def do_HEAD(self):
        if self.path == "/latest":
            self.send_response(302); self.send_header("Location", "/tag/" + latest); self.end_headers(); return
        if self.path.startswith("/tag/"):
            self.send_response(200); self.end_headers(); return
        return super().do_HEAD()
    def translate_path(self, p):
        return os.path.join(root, p.lstrip("/"))
    def log_message(self, *a): pass
srv = http.server.ThreadingHTTPServer(("127.0.0.1", 0), H)
open(os.path.join(root, "..", "port"), "w").write(str(srv.server_address[1]))
srv.serve_forever()
PY
python3 "$WORK/server.py" "$WORK/releases" werkbord-v9.0.2 &
SERVER_PID=$!
i=0; while [ ! -f "$WORK/port" ] && [ $i -lt 50 ]; do sleep 0.1; i=$((i + 1)); done
PORT=$(cat "$WORK/port")
BASE="http://127.0.0.1:$PORT"

freeport() { python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])'; }
# A fresh "computer": its own home, data directory and port.
newhome() {
  H="$WORK/home$1"; mkdir -p "$H"
  export HOME="$H" DEVBOARD_DATA_DIR="$H/data" DEVBOARD_ADDR="127.0.0.1:$(freeport)" DEVBOARD_BASE_URL="$BASE"
  export DEVBOARD_NO_SERVICE=1 DEVBOARD_NO_OPEN=1 DEVBOARD_NO_NETWORK=1
  unset DEVBOARD_VERSION DEVBOARD_NO_SETUP DEVBOARD_INSTALL_DIR || true
}
api() { curl -fsS "http://$DEVBOARD_ADDR$1"; }

# ---- 1. the executable alone ----
newhome 1
out=$(DEVBOARD_NO_SETUP=1 DEVBOARD_VERSION=v9.0.1 $SH scripts/install.sh 2>&1) || bad "install failed" "$out"
[ -x "$HOME/.local/bin/devboard" ] || bad "no executable installed" "$out"
[ "$("$HOME/.local/bin/devboard" version)" = v9.0.1 ] || bad "wrong version installed"
contains "$out" "is not on your PATH" || bad "it should say the directory is not on PATH" "$out"
[ ! -e "$HOME/data" ] || bad "an install-only run created the data directory"
ok "installs only the executable, checked against its checksum"

# ---- 2. nothing is installed from a download that does not match ----
newhome 2
if out=$(DEVBOARD_NO_SETUP=1 DEVBOARD_VERSION=v9.0.3 $SH scripts/install.sh 2>&1); then bad "a download that does not match its checksum was installed" "$out"; fi
contains "$out" "does not match its published checksum" || bad "the refusal should say why" "$out"
[ ! -e "$HOME/.local/bin/devboard" ] || bad "a refused download left an executable"
if out=$(DEVBOARD_NO_SETUP=1 DEVBOARD_VERSION=v9.0.4 $SH scripts/install.sh 2>&1); then bad "an executable of the wrong version was installed" "$out"; fi
contains "$out" "says it is" || bad "the wrong-version refusal should say why" "$out"
[ ! -e "$HOME/.local/bin/devboard" ] || bad "a refused executable was installed"
ok "refuses a download that does not match its checksum, or an executable that is not the version it claims"

# ---- 3. a platform with no build, and a bad version ----
newhome 3
if out=$(DEVBOARD_ARCH=riscv64 $SH scripts/install.sh 2>&1); then bad "an unsupported architecture was accepted"; fi
contains "$out" "no release is built for riscv64" || bad "unsupported arch message" "$out"
if out=$(DEVBOARD_OS=Windows_NT $SH scripts/install.sh 2>&1); then bad "Windows was accepted by the shell installer"; fi
contains "$out" "install.ps1" || bad "it should point Windows users at install.ps1" "$out"
if out=$(DEVBOARD_NO_SETUP=1 DEVBOARD_VERSION=latest $SH scripts/install.sh 2>&1); then bad "a version that is not one was accepted"; fi
if out=$(DEVBOARD_NO_SETUP=1 DEVBOARD_VERSION=v9.9.9 $SH scripts/install.sh 2>&1); then bad "a release that does not exist was installed"; fi
if out=$(DEVBOARD_NO_SETUP=1 DEVBOARD_VERSION=werkbord-team-v0.1.0 $SH scripts/install.sh 2>&1); then bad "a Werkbord Team release was installed as the individual product"; fi
contains "$out" "Werkbord Team release" || bad "it should say a Team release is not for this installer" "$out"
ok "explains a platform or version it cannot install"

# ---- 4. a clean machine, all the way to a running controller ----
newhome 4
out=$($SH scripts/install.sh 2>&1) || bad "the full install failed" "$out"
contains "$out" "Installed $HOME/.local/bin/devboard" || bad "install output" "$out"
contains "$out" "Setting up Dev Board v9.0.2" || bad "setup did not run after the install" "$out"
[ -f "$HOME/data/devboard.db" ] || bad "the database was not created" "$out"
[ -f "$HOME/data/config.json" ] || bad "config.json was not created"
[ -f "$HOME/data/token" ] || bad "the access token was not created"
health=$(api /api/health) || bad "the controller is not answering" "$out"
contains "$health" '"version":"v9.0.2"' || bad "the controller is not the installed version" "$health"
contains "$health" '"status":"ok"' || bad "the controller is unhealthy" "$health"
TOKEN=$(cat "$HOME/data/token")
runners=$(curl -fsS -H "Authorization: Bearer $TOKEN" "http://$DEVBOARD_ADDR/api/runners") || bad "runners"
contains "$runners" '"kind":"local"' || bad "this computer was not registered as a runner" "$runners"
contains "$runners" '"online":true' || bad "the runner is not online" "$runners"
if contains "$out" "$TOKEN"; then bad "the installer printed the access token" "$out"; fi
status=$("$HOME/.local/bin/devboard" status 2>&1) || bad "devboard status failed" "$status"
contains "$status" "running at" || bad "status output" "$status"
ok "a clean install ends with a running controller, a database and this computer as a runner"

# ---- 5. an upgrade replaces the running controller, keeping the data ----
newhome 5
DEVBOARD_VERSION=v9.0.1 $SH scripts/install.sh >/dev/null 2>&1 || bad "installing v9.0.1 failed"
health=$(api /api/health); contains "$health" '"version":"v9.0.1"' || bad "v9.0.1 is not running" "$health"
before=$(ls -l "$HOME/data/devboard.db" | awk '{print $5}')
TOKEN=$(cat "$HOME/data/token")
# some data worth keeping: a project
git init -q -b main "$HOME/repo" && git -C "$HOME/repo" -c user.name=t -c user.email=t@e.com commit -q --allow-empty -m init
curl -fsS -X POST -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' "http://$DEVBOARD_ADDR/api/projects" -d "{\"path\":\"$HOME/repo\"}" >/dev/null || bad "could not register a project"
out=$(DEVBOARD_VERSION=v9.0.2 $SH scripts/install.sh 2>&1) || bad "upgrading failed" "$out"
i=0; while [ $i -lt 100 ]; do health=$(api /api/health 2>/dev/null || true); contains "$health" 'v9.0.2' && break; sleep 0.2; i=$((i + 1)); done
contains "$health" '"version":"v9.0.2"' || bad "the controller was not replaced by the new version" "$health"
projects=$(curl -fsS -H "Authorization: Bearer $TOKEN" "http://$DEVBOARD_ADDR/api/projects")
contains "$projects" '"name":"repo"' || bad "the project was lost in the upgrade" "$projects"
[ "$(cat "$HOME/data/token")" = "$TOKEN" ] || bad "the upgrade changed the access token"
# one controller per data directory; the other tests' controllers may still be up, so only this one is checked
pid=$(cat "$HOME/data/controller.pid"); kill -0 "$pid" 2>/dev/null || bad "the new controller is not the one recorded"
ok "an upgrade restarts the controller on the new version and keeps projects and token"

# ---- 6. stop and start through the commands ----
"$HOME/.local/bin/devboard" stop >/dev/null 2>&1 || bad "stop failed"
if api /api/health >/dev/null 2>&1; then bad "the controller still answers after stop"; fi
"$HOME/.local/bin/devboard" start >/dev/null 2>&1 || bad "start failed"
health=$(api /api/health) || bad "start did not bring it back"
projects=$(curl -fsS -H "Authorization: Bearer $TOKEN" "http://$DEVBOARD_ADDR/api/projects")
contains "$projects" '"name":"repo"' || bad "the project did not survive a restart" "$projects"
"$HOME/.local/bin/devboard" stop >/dev/null 2>&1
ok "stop and start work, and projects survive a restart"

printf '\n%s installer checks passed\n' "$pass"
