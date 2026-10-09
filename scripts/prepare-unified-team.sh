#!/bin/sh
# Fetch only the compatible, separately reviewed Team installer. No private keys,
# release publication or installation occurs here. Used by the Individual signing job.
set -eu
cd "$(dirname "$0")/.."
tag=$(scripts/product.sh werkbord-team tag)
version=$(scripts/product.sh werkbord-team version)
out=${1:?output directory required}
mkdir -p "$out"
out=$(cd "$out" && pwd)
asset="WerkbordTeam_${version}_darwin_universal.dmg"
base="https://github.com/micho8cho93/werkbord/releases/download/$tag"
curl -fsSL "$base/$asset" -o "$out/$asset"
curl -fsSL "$base/$asset.sha256" -o "$out/$asset.sha256"
(cd "$out" && shasum -a 256 -c "$asset.sha256")
scripts/check-desktop-signature.sh --distribution "$out/$asset"
xcrun stapler validate "$out/$asset"
spctl --assess --type open --context context:primary-signature --verbose=2 "$out/$asset"
mkdir "$out/mounted"
hdiutil attach -nobrowse -readonly -mountpoint "$out/mounted" "$out/$asset"
trap 'hdiutil detach "$out/mounted" -quiet' EXIT INT TERM
app="$out/mounted/Werkbord Team.app"
scripts/check-team-desktop.sh --distribution "$app"
[ -n "${TEAM_RELEASE_PUBLIC_KEY:-}" ] || { echo 'independently trusted TEAM_RELEASE_PUBLIC_KEY is required' >&2; exit 1; }
go run ./cmd/werkbord-team/vendor verify-desktop-release --contents "$app/Contents" --version "v$version" --public-key "$TEAM_RELEASE_PUBLIC_KEY"
"$app/Contents/MacOS/Werkbord Team" --verify-release
xcrun stapler validate "$app"
spctl --assess --type execute --verbose=2 "$app"
[ "$("$app/Contents/Helpers/werkbord-team" version)" = "v$version" ]
ditto "$app" "$out/Werkbord Team.app"
