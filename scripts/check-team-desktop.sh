#!/bin/sh
# Inspect a Team bundle without opening it or installing a service. Upstream Nebula stays unmodified and hash pinned.
set -eu
cd "$(dirname "$0")/.."
die() { printf 'check-team-desktop: %s\n' "$*" >&2; exit 1; }
MODE=${1:-}; APP=${2:-}
case "$MODE" in --adhoc|--distribution) ;; *) die "usage: check-team-desktop.sh --adhoc|--distribution <app>" ;; esac
[ -d "$APP/Contents" ] || die "no Team bundle at $APP"
H="$APP/Contents/Helpers"
/usr/bin/codesign --verify --strict --deep "$APP" || die "bundle signature does not verify"
PB=/usr/libexec/PlistBuddy
[ "$($PB -c 'Print :CFBundleIdentifier' "$APP/Contents/Info.plist")" = dev.werkbord.team.desktop ] || die "wrong product identity"
[ "$($PB -c 'Print :CFBundleURLTypes:0:CFBundleURLSchemes:0' "$APP/Contents/Info.plist")" = werkbord ] || die "join links are not registered"
PIN=$(sed -n '/"darwin\/arm64":/s/.*BinarySHA256: "\([a-f0-9]*\)".*/\1/p' internal/team/infra/nebula/manifest.go)
[ "$(shasum -a 256 "$H/nebula" | cut -d' ' -f1)" = "$PIN" ] || die "the network program is not the original pinned release"
/usr/bin/codesign --verify --strict "$H/nebula" || die "upstream network signature does not verify"
TEAM=""
for f in "$APP/Contents/MacOS/Werkbord Team" "$H/werkbord-team" "$H/rqlited" "$H/werkbord"; do
  [ -f "$f" ] || die "missing component $f"
  /usr/bin/codesign --verify --strict "$f" || die "component signature does not verify"
  INFO=$(/usr/bin/codesign -dvv "$f" 2>&1)
  if [ "$MODE" = --distribution ]; then
    case "$INFO" in *Signature=adhoc*) die "ad hoc code in a distribution" ;; esac
    case "$INFO" in *'Authority=Developer ID Application: '* ) ;; *) die "component lacks Developer ID" ;; esac
    case "$INFO" in *'(runtime)'*) ;; *) die "component lacks hardened runtime" ;; esac
    case "$INFO" in *Timestamp=*) ;; *) die "component lacks a secure timestamp" ;; esac
    T=$(printf '%s\n' "$INFO" | sed -n 's/^TeamIdentifier=//p')
    [ -n "$T" ] && [ "$T" != 'not set' ] || die "component lacks a signing team"
    [ -z "$TEAM" ] || [ "$T" = "$TEAM" ] || die "components have different publishers"
    TEAM=$T
  fi
done
if [ "$MODE" = --distribution ]; then
  [ "$(lipo -archs "$H/werkbord-team")" = 'x86_64 arm64' ] || [ "$(lipo -archs "$H/werkbord-team")" = 'arm64 x86_64' ] || die "distribution is not universal"
fi
printf 'Verified separate Team bundle and all required components.\n'
