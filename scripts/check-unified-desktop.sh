#!/bin/sh
# Validate the bundled components (versions, and for a release their signatures) without activation or credentials.
set -eu
cd "$(dirname "$0")/.."
mode=${1:?--development or --release required}
app=${2:?Werkbord app required}
case "$mode" in --development|--release) ;; *) exit 1 ;; esac
die() { printf 'check-unified-desktop: %s\n' "$*" >&2; exit 1; }
[ -f "$app/Contents/Helpers/werkbord-team" ] || die "Team's service is missing from the app"
personal=$("$app/Contents/Helpers/werkbord" version)
team_version=$("$app/Contents/Helpers/werkbord-team" version)
# One release, one version: both executables in the bundle are the release's, so they report the same one.
[ "$personal" = "$team_version" ] || die "Personal ($personal) and Team ($team_version) are not the same release"
grep -Fx "Personal: $personal" "$app/Contents/Resources/components.txt" >/dev/null || die "Personal diagnostics do not match executable"
grep -Fx "Team: $team_version" "$app/Contents/Resources/components.txt" >/dev/null || die "Team diagnostics do not match executable"
cmp desktop/build/compatibility.json "$app/Contents/Resources/compatibility.json" || die "compatibility matrix differs from reviewed source"
if [ "$mode" = --release ]; then
  scripts/check-team-payload.sh --distribution "$app"
  "$app/Contents/MacOS/Werkbord" --verify-release
fi
printf 'Verified unified components: Personal %s; Team %s. No service started.\n' "$personal" "$team_version"
