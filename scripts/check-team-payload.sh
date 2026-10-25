#!/bin/sh
# Inspects signed helpers; executes only the signature-validated service's public metadata probe.
# Each helper must match its trusted runtime pin. Never trust adjacent build records.
#
#   scripts/check-team-payload.sh --adhoc|--distribution <Werkbord.app>
set -eu
cd "$(dirname "$0")/.."
die() { printf 'check-team-payload: %s\n' "$*" >&2; exit 1; }
MODE=${1:-}; APP=${2:-}
case "$MODE" in --adhoc|--distribution) ;; *) die "usage: check-team-payload.sh --adhoc|--distribution <Werkbord.app>" ;; esac
[ -d "$APP/Contents" ] || die "no app at $APP"
CODESIGN=${CODESIGN:-/usr/bin/codesign}
H="$APP/Contents/Helpers"
for f in "$H/werkbord-team" "$H/nebula" "$H/rqlited" "$APP/Contents/Resources/rqlited.build"; do
  [ -f "$f" ] || die "missing $f"
done
"$CODESIGN" --verify --strict "$H/werkbord-team" || die "the service signature must verify before probing public build metadata"
python3 scripts/check-embedded-pins.py "$H/werkbord-team" "$H/nebula" "$H/rqlited" || die "helper bytes differ from the service's trusted build pins"
# The database program is stamped into the service by its hash; the service refuses any other.
want=$(sed -n 's/.*"sha256":"\([a-f0-9]*\)".*/\1/p' "$APP/Contents/Resources/rqlited.build")
[ "$(shasum -a 256 "$H/rqlited" | cut -d' ' -f1)" = "$want" ] || die "the database program is not the one its build record names"
for f in "$H/werkbord-team" "$H/nebula" "$H/rqlited"; do
  "$CODESIGN" --verify --strict "$f" || die "$f's signature does not verify"
done
if [ "$MODE" = --distribution ]; then
  archs=$(lipo -archs "$H/werkbord-team")
  case "$archs" in "x86_64 arm64"|"arm64 x86_64") ;; *) die "Team's service is not universal" ;; esac
fi
printf 'Verified the Team service in %s.\n' "$APP"
