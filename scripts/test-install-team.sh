#!/bin/sh
# Tests Team's installer against a release server on this computer: nothing is
# downloaded from the Internet and nothing outside a temporary directory is touched.
#
#   scripts/test-install-team.sh
#
# The server holds an individual release and Team releases side by side, as the
# real releases page does, to prove the two products are never mixed up.
set -eu
SH=${SH:-sh}
cd "$(dirname "$0")/.."
WORK=$(mktemp -d)
SERVER_PID=""
cleanup() { [ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null || true; rm -rf "$WORK"; }
trap cleanup EXIT INT TERM

pass=0
ok() { pass=$((pass + 1)); printf '  ok  %s\n' "$1"; }
bad() { printf 'FAIL  %s\n' "$1" >&2; [ -n "${2:-}" ] && printf '%s\n' "$2" >&2; exit 1; }
contains() { case "$1" in *"$2"*) return 0 ;; esac; return 1; }

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$(uname -m)" in x86_64|amd64) arch=amd64 ;; arm64|aarch64) arch=arm64 ;; *) echo "unsupported test machine"; exit 1 ;; esac

mkdir -p "$WORK/releases"
for v in v9.1.0 v9.1.1; do
  BUNDLE_NEBULA=no PLATFORMS="$os/$arch" scripts/build-release.sh werkbord-team "$v" "$WORK/build-$v" >/dev/null
  mkdir -p "$WORK/releases/werkbord-team-$v"
  cp "$WORK/build-$v"/* "$WORK/releases/werkbord-team-$v/"
done
# An individual release exists too and is the newest thing on the page.
mkdir -p "$WORK/releases/werkbord-v9.2.0"
# A Team release whose archive is not what its checksum says.
mkdir -p "$WORK/releases/werkbord-team-v9.1.2"
cp "$WORK/releases/werkbord-team-v9.1.1/werkbord-team_9.1.1_${os}_${arch}.tar.gz" "$WORK/releases/werkbord-team-v9.1.2/werkbord-team_9.1.2_${os}_${arch}.tar.gz"
printf '%s  werkbord-team_9.1.2_%s_%s.tar.gz\n' "$(printf 'something else' | shasum -a 256 2>/dev/null | cut -d' ' -f1 || printf 'something else' | sha256sum | cut -d' ' -f1)" "$os" "$arch" > "$WORK/releases/werkbord-team-v9.1.2/checksums.txt"
# A Team release whose executable reports another version.
mkdir -p "$WORK/releases/werkbord-team-v9.1.3"
cp "$WORK/releases/werkbord-team-v9.1.1/werkbord-team_9.1.1_${os}_${arch}.tar.gz" "$WORK/releases/werkbord-team-v9.1.3/werkbord-team_9.1.3_${os}_${arch}.tar.gz"
sum=$(shasum -a 256 "$WORK/releases/werkbord-team-v9.1.3/werkbord-team_9.1.3_${os}_${arch}.tar.gz" 2>/dev/null || sha256sum "$WORK/releases/werkbord-team-v9.1.3/werkbord-team_9.1.3_${os}_${arch}.tar.gz")
printf '%s  werkbord-team_9.1.3_%s_%s.tar.gz\n' "$(printf '%s' "$sum" | cut -d' ' -f1)" "$os" "$arch" > "$WORK/releases/werkbord-team-v9.1.3/checksums.txt"
ok "built the test releases"

# Release notes mention historical Team tags and comparison URLs before the real
# Team entry. A prerelease also precedes the stable release. Only release links
# identify the default installation, including compact/multiline Atom layouts.
cat > "$WORK/releases/feed.atom" <<'ATOM'
<feed><entry><link href="/releases/tag/werkbord-v9.2.0"/>
<content type="html">&lt;a href=&quot;/compare/werkbord-team-v9.1.0...werkbord-v9.2.0&quot;&gt;Changelog&lt;/a&gt; mentions werkbord-team-v99.0.0</content></entry>
<entry><link href="/releases/tag/werkbord-team-v9.1.2-rc.1"/></entry>
<entry><link rel="alternate"
 href="/releases/tag/werkbord-team-v9.1.1"/></entry><entry><link href="/releases/tag/werkbord-team-v9.1.0"/></entry></feed>
ATOM

cat > "$WORK/server.py" <<'PY'
import http.server, os, sys
root = sys.argv[1]
class H(http.server.SimpleHTTPRequestHandler):
    def do_GET(self):
        if self.path.startswith("/download/"):
            self.path = self.path[len("/download"):]
        return super().do_GET()
    def translate_path(self, p):
        return os.path.join(root, p.lstrip("/"))
    def log_message(self, *a): pass
srv = http.server.ThreadingHTTPServer(("127.0.0.1", 0), H)
tmp = os.path.join(root, "..", "port.tmp")
open(tmp, "w").write(str(srv.server_address[1]))
os.rename(tmp, os.path.join(root, "..", "port"))
srv.serve_forever()
PY
python3 "$WORK/server.py" "$WORK/releases" &
SERVER_PID=$!
# Python can take several seconds to start on a busy CI machine: wait up to 30s, and say so if it never comes up.
i=0; while [ ! -s "$WORK/port" ] && kill -0 "$SERVER_PID" 2>/dev/null && [ $i -lt 300 ]; do sleep 0.1; i=$((i + 1)); done
[ -s "$WORK/port" ] || bad "the test release server did not start (python3 $(python3 --version 2>&1), pid $SERVER_PID)"
PORT=$(cat "$WORK/port")
export WERKBORD_TEAM_BASE_URL="http://127.0.0.1:$PORT"
export WERKBORD_TEAM_FEED_URL="http://127.0.0.1:$PORT/feed.atom"

fresh() { rm -rf "$WORK/home"; mkdir -p "$WORK/home"; export HOME="$WORK/home"; unset WERKBORD_TEAM_VERSION WERKBORD_TEAM_INSTALL_DIR || true; }

# 1. the latest TEAM release, though an individual release is newer
fresh
out=$($SH scripts/install-team.sh 2>&1) || bad "install failed" "$out"
[ "$("$HOME/.local/bin/werkbord-team" version)" = v9.1.1 ] || bad "the latest Team release (v9.1.1) was not installed" "$out"
[ ! -e "$HOME/.local/bin/werkbord" ] && [ ! -e "$HOME/.local/bin/devboard" ] || bad "the individual product was installed"
ok "installs the stable Team release, ignoring changelog tags and prereleases"

# 1b. a release that carries the network program installs it beside the executable, and its licences.
# (The program in this archive is a stand-in: the installer checks the archive's checksum, and Team checks the program
# against its pin when it runs; what is tested here is where the files go.)
mkdir -p "$WORK/bundle/pkg/libexec/werkbord-team" "$WORK/bundle/pkg/licenses/nebula" "$WORK/releases/werkbord-team-v9.1.4"
tar -xzf "$WORK/releases/werkbord-team-v9.1.1/werkbord-team_9.1.1_${os}_${arch}.tar.gz" -C "$WORK/bundle/pkg"
printf '#!/bin/sh\necho stand-in\n' > "$WORK/bundle/pkg/libexec/werkbord-team/nebula"
printf 'MIT License\n' > "$WORK/bundle/pkg/licenses/nebula/LICENSE"
printf '#!/bin/sh\necho stand-in\n' > "$WORK/bundle/pkg/libexec/werkbord-team/rqlited"
# (the executable says v9.1.1; the installer would refuse it as v9.1.4, so install this one by its real name)
tar -czf "$WORK/releases/werkbord-team-v9.1.4/werkbord-team_9.1.4_${os}_${arch}.tar.gz" -C "$WORK/bundle/pkg" werkbord-team README.md libexec licenses
sum=$(shasum -a 256 "$WORK/releases/werkbord-team-v9.1.4/werkbord-team_9.1.4_${os}_${arch}.tar.gz" 2>/dev/null || sha256sum "$WORK/releases/werkbord-team-v9.1.4/werkbord-team_9.1.4_${os}_${arch}.tar.gz")
printf '%s  werkbord-team_9.1.4_%s_%s.tar.gz\n' "$(printf '%s' "$sum" | cut -d' ' -f1)" "$os" "$arch" > "$WORK/releases/werkbord-team-v9.1.4/checksums.txt"
fresh
if out=$(WERKBORD_TEAM_VERSION=v9.1.4 $SH scripts/install-team.sh 2>&1); then bad "an executable of the wrong version was installed" "$out"; fi
contains "$out" "not installing it" || bad "wrong refusal" "$out"
[ ! -e "$HOME/.local/libexec/werkbord-team/nebula" ] || bad "the network program was installed from a release that was refused"
[ ! -e "$HOME/.local/libexec/werkbord-team/rqlited" ] || bad "the database program was installed from a release that was refused"
# the same archive with an executable that tells the truth
printf '#!/bin/sh\n[ "$1" = version ] && echo v9.1.4\n' > "$WORK/bundle/pkg/werkbord-team"
chmod 755 "$WORK/bundle/pkg/werkbord-team"
tar -czf "$WORK/releases/werkbord-team-v9.1.4/werkbord-team_9.1.4_${os}_${arch}.tar.gz" -C "$WORK/bundle/pkg" werkbord-team README.md libexec licenses
sum=$(shasum -a 256 "$WORK/releases/werkbord-team-v9.1.4/werkbord-team_9.1.4_${os}_${arch}.tar.gz" 2>/dev/null || sha256sum "$WORK/releases/werkbord-team-v9.1.4/werkbord-team_9.1.4_${os}_${arch}.tar.gz")
printf '%s  werkbord-team_9.1.4_%s_%s.tar.gz\n' "$(printf '%s' "$sum" | cut -d' ' -f1)" "$os" "$arch" > "$WORK/releases/werkbord-team-v9.1.4/checksums.txt"
fresh
WERKBORD_TEAM_VERSION=v9.1.4 $SH scripts/install-team.sh >/dev/null 2>&1 || bad "installing the release that carries the network program failed"
[ -x "$HOME/.local/libexec/werkbord-team/nebula" ] || bad "the network program was not installed beside the executable" "$(ls -R "$HOME/.local" 2>&1)"
[ -x "$HOME/.local/libexec/werkbord-team/rqlited" ] || bad "the database program was not installed beside the executable" "$(ls -R "$HOME/.local" 2>&1)"
[ -f "$HOME/.local/share/doc/werkbord-team/licenses/nebula/LICENSE" ] || bad "the licences were not installed"
ok "installs the network program, the database program and their licences from a release that carries them, and from no other"

# 2. a named version, in either spelling
fresh
WERKBORD_TEAM_VERSION=v9.1.0 $SH scripts/install-team.sh >/dev/null 2>&1 || bad "installing v9.1.0 failed"
[ "$("$HOME/.local/bin/werkbord-team" version)" = v9.1.0 ] || bad "wrong version installed"
WERKBORD_TEAM_VERSION=werkbord-team-v9.1.1 $SH scripts/install-team.sh >/dev/null 2>&1 || bad "installing by tag failed"
[ "$("$HOME/.local/bin/werkbord-team" version)" = v9.1.1 ] || bad "upgrade by tag did not take"
ok "installs a named version, by version or by tag, and upgrades in place"

# 3. refusals
fresh
if out=$(WERKBORD_TEAM_VERSION=v9.1.2 $SH scripts/install-team.sh 2>&1); then bad "a download that does not match its checksum was installed" "$out"; fi
contains "$out" "does not match its published checksum" || bad "wrong refusal" "$out"
if out=$(WERKBORD_TEAM_VERSION=v9.1.3 $SH scripts/install-team.sh 2>&1); then bad "an executable of the wrong version was installed" "$out"; fi
contains "$out" "not installing it" || bad "wrong refusal" "$out"
[ ! -e "$HOME/.local/bin/werkbord-team" ] || bad "a refused executable was installed"
if out=$(WERKBORD_TEAM_VERSION=werkbord-v9.2.0 $SH scripts/install-team.sh 2>&1); then bad "an individual release was installed as Team" "$out"; fi
contains "$out" "individual Werkbord release" || bad "it should say the release is the individual product's" "$out"
if out=$(WERKBORD_TEAM_VERSION=latest $SH scripts/install-team.sh 2>&1); then bad "a version that is not one was accepted"; fi
if out=$(WERKBORD_TEAM_VERSION=v9.9.9 $SH scripts/install-team.sh 2>&1); then bad "a release that does not exist was installed"; fi
if out=$(WERKBORD_TEAM_ARCH=riscv64 $SH scripts/install-team.sh 2>&1); then bad "an unsupported architecture was accepted"; fi
ok "refuses a bad checksum, a lying executable, another product's release, and what does not exist"

# 4. the individual installer refuses a Team release in the same way
fresh
if out=$(DEVBOARD_NO_SETUP=1 DEVBOARD_BASE_URL="http://127.0.0.1:$PORT" DEVBOARD_VERSION=werkbord-team-v9.1.1 $SH scripts/install.sh 2>&1); then bad "the individual installer installed a Team release" "$out"; fi
contains "$out" "Werkbord Team release" || bad "the individual installer should say it is a Team release" "$out"
ok "the individual installer refuses a Team release"

# 5. Descriptions alone must never become downloadable release identities.
fresh
cat > "$WORK/releases/feed.atom" <<'ATOM'
<feed><entry><link href="/releases/tag/werkbord-v9.2.0"/><content type="html">werkbord-team-v9.1.1</content></entry></feed>
ATOM
if out=$($SH scripts/install-team.sh 2>&1); then bad "a description tag was treated as a Team release" "$out"; fi
contains "$out" "no Werkbord Team release has been published yet" || bad "wrong empty Team feed refusal" "$out"
[ ! -e "$HOME/.local/bin/werkbord-team" ] || bad "an executable was installed from a description tag"
ok "refuses a feed that mentions Team only in release descriptions"

printf '\nall %d checks passed\n' "$pass"
