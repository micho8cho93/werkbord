#!/bin/sh
# Validate actual bundled component versions without activation or credentials.
set -eu
cd "$(dirname "$0")/.."
mode=${1:?--development or --release required}
app=${2:?Werkbord app required}
case "$mode" in --development|--release) ;; *) exit 1 ;; esac
die() { printf 'check-unified-desktop: %s\n' "$*" >&2; exit 1; }
team="$app/Contents/Helpers/Werkbord Team.app"
[ -d "$team" ] || die "Team payload missing"
if [ "$mode" = --release ]; then
  [ -n "${TEAM_RELEASE_PUBLIC_KEY:-}" ] || die "independently trusted TEAM_RELEASE_PUBLIC_KEY is required"
  # Authenticate fixed payload paths before executing any downloaded Team helper.
  go run ./cmd/werkbord-team/vendor verify-desktop-release --contents "$team/Contents" --version "v$(scripts/product.sh werkbord-team version)" --public-key "$TEAM_RELEASE_PUBLIC_KEY"
fi
personal=$("$app/Contents/Helpers/werkbord" version)
team_version=$("$team/Contents/Helpers/werkbord-team" version)
# One release, one version: both executables in the bundle are the release's, so they report the same one.
[ "$personal" = "$team_version" ] || die "Personal ($personal) and Team ($team_version) are not the same release"
grep -Fx "Personal: $personal" "$app/Contents/Resources/components.txt" >/dev/null || die "Personal diagnostics do not match executable"
grep -Fx "Team: $team_version" "$app/Contents/Resources/components.txt" >/dev/null || die "Team diagnostics do not match executable"
cmp desktop/build/compatibility.json "$app/Contents/Resources/compatibility.json" || die "compatibility matrix differs from reviewed source"
if [ "$mode" = --release ]; then
  scripts/check-team-desktop.sh --distribution "$team"
  "$team/Contents/MacOS/Werkbord Team" --verify-release
fi
printf 'Verified unified components: Personal %s; Team %s. No service started.\n' "$personal" "$team_version"
