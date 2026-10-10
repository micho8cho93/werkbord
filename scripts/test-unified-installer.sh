#!/bin/sh
# Real macOS packaging checks. This mounts a local development DMG; it never installs
# into Applications, asks for administrator rights, or modifies the user's services.
set -eu
cd "$(dirname "$0")/.."
[ "$(uname -s)" = Darwin ] || { echo 'unified installer test requires macOS'; exit 1; }
app=${1:-dist/desktop/Werkbord.app}
scripts/check-unified-desktop.sh --development "$app"
scripts/check-desktop-signature.sh --adhoc "$app"
team="$app/Contents/Helpers/Werkbord Team.app"
if "$team/Contents/MacOS/Werkbord Team" --verify-release >/dev/null 2>&1; then
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
scripts/check-unified-desktop.sh --development "$work/mounted/Werkbord.app"
scripts/check-desktop-signature.sh --adhoc "$work/mounted/Werkbord.app"
echo 'PASS real unified development installer, checksum, mounted bundle, versions and development-build refusal'
