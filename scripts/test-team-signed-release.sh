#!/bin/sh
# Native signed-release smoke test. Real sidecars, isolated install/data directories,
# ephemeral fixture issuers, no OS service or privileged network interface.
set -eu
umask 077
cd "$(dirname "$0")/.."
WORK=$(mktemp -d)
SERVER_PID=""; TEAM_PID=""
cleanup() {
  [ -z "$TEAM_PID" ] || { kill "$TEAM_PID" 2>/dev/null || true; wait "$TEAM_PID" 2>/dev/null || true; }
  [ -z "$SERVER_PID" ] || { kill "$SERVER_PID" 2>/dev/null || true; wait "$SERVER_PID" 2>/dev/null || true; }
  rm -rf "$WORK"
}
trap cleanup EXIT INT TERM
die() { printf 'signed-release test: %s\n' "$*" >&2; exit 1; }
os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$(uname -m)" in arm64|aarch64) arch=arm64 ;; x86_64|amd64) arch=amd64 ;; *) die "unsupported architecture" ;; esac
version=v$(scripts/product.sh werkbord-team version)
tag=werkbord-$version
openssl genpkey -algorithm ED25519 -out "$WORK/license.pem" >/dev/null 2>&1
openssl pkey -in "$WORK/license.pem" -pubout -outform DER -out "$WORK/license-public.der" >/dev/null 2>&1
openssl genpkey -algorithm ED25519 -out "$WORK/release.pem" >/dev/null 2>&1
openssl pkey -in "$WORK/release.pem" -pubout -out "$WORK/release.pub" >/dev/null 2>&1
LICENSE_ISSUER_PUBLIC_KEY=$(python3 - "$WORK/license-public.der" <<'PY'
import base64, pathlib, sys
print(base64.urlsafe_b64encode(pathlib.Path(sys.argv[1]).read_bytes()[-32:]).decode().rstrip('='))
PY
)
export LICENSE_ISSUER_PUBLIC_KEY
cat > "$WORK/claims.json" <<'JSON'
{"schema":2,"product":"werkbord-team","id":"lic_release_fixture","customer":"org_fixture","edition":"team","seats":3,"issuedAt":"2026-01-01T00:00:00Z"}
JSON
go run ./cmd/werkbord-team/vendor license --input "$WORK/claims.json" --out "$WORK/license.json" < "$WORK/license.pem"
PLATFORMS="$os/$arch" WERKBORD_TEAM_RELEASE_SIGNING_KEY_FILE="$WORK/release.pem" \
  scripts/build-release.sh werkbord-team "$version" "$WORK/releases/$tag"
cat > "$WORK/serve.py" <<'PY'
import http.server, os, sys
root, portfile = sys.argv[1:]
class Handler(http.server.SimpleHTTPRequestHandler):
    def do_GET(self):
        if self.path.startswith('/download/'):
            self.path = self.path[len('/download'):]
        super().do_GET()
    def translate_path(self, path):
        return os.path.join(root, path.lstrip('/'))
    def log_message(self, *args): pass
server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
with open(portfile, 'w') as f: f.write(str(server.server_address[1]))
server.serve_forever()
PY
python3 "$WORK/serve.py" "$WORK/releases" "$WORK/port" &
SERVER_PID=$!
tries=0
while [ ! -s "$WORK/port" ] && kill -0 "$SERVER_PID" 2>/dev/null && [ "$tries" -lt 300 ]; do
  sleep .1; tries=$((tries+1))
done
[ -s "$WORK/port" ] || die "fixture release server did not start"
WERKBORD_TEAM_RELEASE_PUBLIC_KEY_FILE="$WORK/release.pub" \
WERKBORD_TEAM_INSTALL_DIR="$WORK/install/bin" \
WERKBORD_TEAM_BASE_URL="http://127.0.0.1:$(cat "$WORK/port")" \
WERKBORD_TEAM_VERSION="$tag" sh scripts/install-team.sh >/dev/null
bin="$WORK/install/bin/werkbord-team"
[ "$("$bin" version)" = "$version" ] || die "installed version differs"
[ -x "$WORK/install/libexec/werkbord-team/nebula" ] && [ -x "$WORK/install/libexec/werkbord-team/rqlited" ] || die "required sidecars absent"
[ -f "$WORK/install/share/doc/werkbord-team/licenses/rqlite/LICENSE" ] || die "sidecar notice absent"
openssl rand -base64 32 > "$WORK/unlock"
ports=$(python3 - <<'PY'
import socket
sockets=[socket.socket() for _ in range(4)]
for s in sockets: s.bind(('127.0.0.1',0))
print(' '.join(str(s.getsockname()[1]) for s in sockets))
for s in sockets: s.close()
PY
)
# The four ports were allocated together; nothing is bound to a production port.
# shellcheck disable=SC2086
set -- $ports
export WERKBORD_TEAM_DATA_DIR="$WORK/workspace" WERKBORD_TEAM_LICENSE_FILE="$WORK/license.json"
export WERKBORD_TEAM_PKI_PASSPHRASE_FILE="$WORK/unlock" WERKBORD_TEAM_NETWORK_NODE=off
export WERKBORD_TEAM_ADDR="127.0.0.1:$1" WERKBORD_TEAM_BOOTSTRAP_ADDR="127.0.0.1:$4"
"$bin" workspace create --name Gate --owner Owner --endpoint 127.0.0.1 --storage-port "$2" --storage-raft-port "$3" > "$WORK/created.txt"
"$bin" serve > "$WORK/team.log" 2>&1 &
TEAM_PID=$!
python3 - "$WORK/created.txt" "$WERKBORD_TEAM_ADDR" <<'PY'
import json, pathlib, re, sys, time, urllib.request
token=re.search(r'\bwbt_[a-f0-9]{64}\b',pathlib.Path(sys.argv[1]).read_text()).group()
base='http://'+sys.argv[2]+'/api/team/v1'
def get(path):
    r=urllib.request.Request(base+path,headers={'Authorization':'Bearer '+token})
    with urllib.request.urlopen(r,timeout=2) as response: return json.load(response)
for _ in range(180):
    try:
        assert get('/me')['workspace']['name']=='Gate'
        assert get('/network')['settings']['enrollmentApproval']=='admin'
        break
    except Exception: time.sleep(.5)
else: raise SystemExit('installed host did not authenticate against its current database')
PY
kill "$TEAM_PID"; wait "$TEAM_PID"; TEAM_PID=""
mv "$WORK/install/libexec/werkbord-team/rqlited" "$WORK/original-rqlited"
printf '#!/bin/sh\ntouch "%s/was-executed"\n' "$WORK" > "$WORK/install/libexec/werkbord-team/rqlited"
chmod 755 "$WORK/install/libexec/werkbord-team/rqlited"
if WERKBORD_TEAM_DATA_DIR="$WORK/refused" "$bin" workspace create --name Replaced --owner Owner --endpoint 127.0.0.1 --storage-port "$2" --storage-raft-port "$3" > "$WORK/refusal.log" 2>&1; then
  die "replaced sidecar was accepted"
fi
[ ! -e "$WORK/was-executed" ] || die "unverified sidecar was executed"
printf 'PASS real vendor-tool signatures, native signed bundle install, licensed default-Nebula workspace create/serve, admin approval and pre-execution sidecar rejection\n'
