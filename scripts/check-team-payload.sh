#!/bin/sh
# Inspects the Team service a Werkbord.app carries, without opening it or installing anything. Upstream Nebula keeps its own
# signature and is held to its pin; the rest of the app's code is checked by scripts/check-desktop-signature.sh.
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
PIN=$(sed -n '/"darwin\/arm64":/s/.*BinarySHA256: "\([a-f0-9]*\)".*/\1/p' internal/team/infra/nebula/manifest.go)
[ -n "$PIN" ] || die "cannot read the network program's pin"
[ "$(shasum -a 256 "$H/nebula" | cut -d' ' -f1)" = "$PIN" ] || die "the network program is not the original pinned release"
"$CODESIGN" --verify --strict "$H/nebula" || die "the network program's upstream signature does not verify"
# The database program is stamped into the service by its hash; the service refuses any other.
want=$(sed -n 's/.*"sha256":"\([a-f0-9]*\)".*/\1/p' "$APP/Contents/Resources/rqlited.build")
[ "$(shasum -a 256 "$H/rqlited" | cut -d' ' -f1)" = "$want" ] || die "the database program is not the one its build record names"
for f in "$H/werkbord-team" "$H/rqlited"; do
  "$CODESIGN" --verify --strict "$f" || die "$f's signature does not verify"
done
if [ "$MODE" = --distribution ]; then
  archs=$(lipo -archs "$H/werkbord-team")
  case "$archs" in "x86_64 arm64"|"arm64 x86_64") ;; *) die "Team's service is not universal" ;; esac
fi
printf 'Verified the Team service in %s.\n' "$APP"
