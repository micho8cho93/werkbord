#!/bin/sh
# Real macOS packaging checks. This mounts a local development DMG; it never installs
# into Applications, asks for administrator rights, or modifies the user's services.
set -eu
cd "$(dirname "$0")/.."
[ "$(uname -s)" = Darwin ] || { echo 'unified installer test requires macOS'; exit 1; }
app=${1:-dist/desktop/Werkbord.app}
# One release: the controller and Team's service in the app report the same version, and the Team service is all there.
same_release() {
  [ -f "$1/Contents/Helpers/werkbord-team" ] || { echo "the app carries no Team service" >&2; exit 1; }
  controller=$("$1/Contents/Helpers/werkbord" version); service=$("$1/Contents/Helpers/werkbord-team" version)
  [ "$controller" = "$service" ] || { echo "the controller ($controller) and Team's service ($service) are not the same release" >&2; exit 1; }
  scripts/check-team-payload.sh --adhoc "$1" >/dev/null
}
same_release "$app"
scripts/check-desktop-signature.sh --adhoc "$app"
if "$app/Contents/MacOS/Werkbord" --verify-release >/dev/null 2>&1; then
  echo 'development fixture unexpectedly accepted as a release' >&2; exit 1
fi
dmg=$(find dist/desktop -maxdepth 1 -name 'Werkbord_*.dmg' | sort | tail -1)
[ -n "$dmg" ] || { echo 'run make desktop-package first' >&2; exit 1; }
(cd dist/desktop && shasum -a 256 -c "$(basename "$dmg").sha256")
work=$(mktemp -d)
trap 'hdiutil detach "$work/mounted" -quiet >/dev/null 2>&1 || true; rm -rf "$work"' EXIT INT TERM
mkdir "$work/mounted"
hdiutil attach -nobrowse -readonly -mountpoint "$work/mounted" "$dmg" -quiet
[ -L "$work/mounted/Applications" ]
same_release "$work/mounted/Werkbord.app"
scripts/check-desktop-signature.sh --adhoc "$work/mounted/Werkbord.app"
echo 'PASS real unified development installer, checksum, mounted bundle, versions and development-build refusal'
