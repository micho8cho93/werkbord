#!/bin/sh
# Makes the appcast for one release: the small signed feed the app reads to learn that an update exists, and where to
# get it. It signs the update archive (EdDSA), and the feed itself, with the private update-signing key, and then checks
# what it made against everything the app will require.
#
#   scripts/make-appcast.sh [--require] <Werkbord_<version>_darwin_<arch>.zip> <outdir>
#
# The key is only ever in the environment, which is where CI puts a secret, and goes to Sparkle's tool on its standard
# input: not on a command line, not in a file, not in a log.
#   SPARKLE_ED_PRIVATE_KEY   the private key, as `generate_keys -x` writes it: one line of base64
#   SPARKLE_ED_KEY_FILE      a file holding it instead (tests only)
# With no key it says so on one line and exits cleanly, making nothing; --require (a release) makes that a failure.
#
# The feed holds exactly one item, the release being published: nothing older that could be offered again, and the
# enclosure is under that release's own download address. outdir/appcast.xml is what to upload to the release as
# "appcast.xml"; https://github.com/micho8cho93/werkbord/releases/latest/download/appcast.xml then serves it, because only
# individual releases are marked "latest".
set -eu

die() { printf 'make-appcast: %s\n' "$*" >&2; exit 1; }

require=""
if [ "${1:-}" = --require ]; then require=1; shift; fi
zip=${1:-}
out=${2:-}
[ -f "$zip" ] && [ -n "$out" ] || die "usage: make-appcast.sh [--require] <Werkbord_<version>_darwin_<arch>.zip> <outdir>"
cd "$(dirname "$0")/.."
ROOT=$(pwd)

if [ -z "${SPARKLE_ED_PRIVATE_KEY:-}" ] && [ -z "${SPARKLE_ED_KEY_FILE:-}" ]; then
  msg="the secret SPARKLE_ED_PRIVATE_KEY is not set (docs/DESKTOP_RELEASE.md, step 6): no appcast can be made, so installed apps would not be offered this release"
  if [ -n "$require" ]; then die "$msg"; fi
  echo "make-appcast: $msg; skipping"
  exit 0
fi

name=$(basename "$zip")
case "$name" in
  Werkbord_[0-9]*.[0-9]*.[0-9]*_darwin_*.zip) ;;
  *) die "$name is not Werkbord_<version>_darwin_<arch>.zip" ;;
esac
version=${name#Werkbord_}; version=${version%%_darwin_*}
tag="werkbord-v$version"
prefix=${DOWNLOAD_URL_PREFIX:-https://github.com/micho8cho93/werkbord/releases/download/$tag/}
case "$prefix" in */) ;; *) prefix="$prefix/" ;; esac
page="https://github.com/micho8cho93/werkbord/releases/tag/$tag"

SPARKLE_HOME=$(scripts/fetch-sparkle.sh) || die "Sparkle's tools are not available"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT INT TERM
mkdir -p "$work/archives" "$work/home" "$out"
cp "$zip" "$work/archives/$name"
# What the update window shows, and no more: no web page is fetched to show it.
cat > "$work/archives/${name%.zip}.html" <<EOF
<h3>Werkbord $version</h3>
<p>Your data is backed up before the program is updated, and that waits while coding agents are working. Your agents and schedules keep running while the app is replaced.</p>
<p><a href="$page">What changed in $version</a></p>
EOF

# generate_appcast writes into the user's caches; keep that out of anybody's real home.
if [ -n "${SPARKLE_ED_KEY_FILE:-}" ]; then
  HOME="$work/home" "$SPARKLE_HOME/bin/generate_appcast" --ed-key-file "$SPARKLE_ED_KEY_FILE" \
    --download-url-prefix "$prefix" --full-release-notes-url "$page" --link "https://github.com/micho8cho93/werkbord" \
    --maximum-versions 1 -o "$work/appcast.xml" "$work/archives" >"$work/gen.log" 2>&1 || { cat "$work/gen.log" >&2; die "generate_appcast failed"; }
else
  printf '%s' "$SPARKLE_ED_PRIVATE_KEY" | HOME="$work/home" "$SPARKLE_HOME/bin/generate_appcast" --ed-key-file - \
    --download-url-prefix "$prefix" --full-release-notes-url "$page" --link "https://github.com/micho8cho93/werkbord" \
    --maximum-versions 1 -o "$work/appcast.xml" "$work/archives" >"$work/gen.log" 2>&1 || { sed 's/[A-Za-z0-9+\/=]\{40,\}/<redacted>/g' "$work/gen.log" >&2; die "generate_appcast failed"; }
fi
[ -s "$work/appcast.xml" ] || die "generate_appcast made no appcast"

# What the app will hold the feed to, checked here: its own public key and version, read out of the archive that is being offered.
unzip -p "$zip" 'Werkbord.app/Contents/Info.plist' > "$work/Info.plist" 2>/dev/null || die "$name does not hold Werkbord.app/Contents/Info.plist"
pub=$(plutil -extract SUPublicEDKey raw -o - "$work/Info.plist" 2>/dev/null) || die "the app in $name has no SUPublicEDKey: it could not verify what this feed offers"
bundle=$(plutil -extract CFBundleVersion raw -o - "$work/Info.plist")
CHECK=$(mktemp "$work/appcast-check.XXXXXX")
go build -o "$CHECK" ./scripts/appcast || die "could not build the appcast checker"
# (A feed on this computer, for the tests, is the one thing that may be plain http.)
loop=""; case "$prefix" in http://127.0.0.1:*) loop=-loopback ;; esac
"$CHECK" check -tag "$tag" -base "$prefix" -pubkey "$pub" -archive "$zip" -bundle-version "$bundle" $loop "$work/appcast.xml" || die "the appcast is not what the app will accept"
cp "$work/appcast.xml" "$out/appcast.xml"
echo "make-appcast: wrote $out/appcast.xml for $tag"
