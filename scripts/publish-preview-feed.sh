#!/bin/sh
# Publishes a verified release's appcast as the PREVIEW feed, the address a preview app looks for updates at
# (scripts/desktop-feed-url.sh). Called by release.yml once the release is public and its own feed and download have been verified.
#
#   GITHUB_REPOSITORY=owner/name GITHUB_SHA=<commit> GH_TOKEN=… scripts/publish-preview-feed.sh <werkbord-vX.Y.Z[-preview.N]>
#
# The preview feed is a release called werkbord-preview-feed: a pre-release, never "latest" (so it can never become the stable feed
# or be found by the controller's own check), holding one asset, appcast.xml. Every published release (stable ones too, so that a
# preview app is carried to a stable release when one is newer) replaces that asset with its own appcast, byte for byte: the
# archive it points at is the release's, the signature is the release's, and the feed still holds that release alone, so an
# older item can never be offered again. It is the LAST step of a release, after the checks, like the stable feed.
#
# It never moves the feed backwards: if the feed already names a newer release (an old run repeated, two tags published close
# together), it leaves it. Afterwards it fetches the feed from the public address and checks it is exactly this release's appcast and
# that everything an installed app holds a feed to still holds (scripts/verify-appcast.sh), and fails if it is not.
#
# Test hooks: GH (the GitHub CLI), VERIFY (scripts/verify-appcast.sh), RETRY_SLEEP (seconds between tries of the public check).
set -eu

die() { printf 'publish-preview-feed: FAILED: %s\n' "$*" >&2; exit 1; }
say() { printf 'publish-preview-feed: %s\n' "$*"; }

FEED_TAG=werkbord-preview-feed
GH=${GH:-gh}
VERIFY=${VERIFY:-scripts/verify-appcast.sh}
RETRY_SLEEP=${RETRY_SLEEP:-10}

tag=${1:-}
case "$tag" in
  werkbord-team-v*) die "$tag is a Werkbord Team release: the Mac app belongs to the individual product" ;;
  werkbord-v[0-9]*.[0-9]*.[0-9]*) ;;
  *) die "usage: publish-preview-feed.sh werkbord-vX.Y.Z[-preview.N]" ;;
esac
repo=${GITHUB_REPOSITORY:-}
[ -n "$repo" ] || die "GITHUB_REPOSITORY is not set"
cd "$(dirname "$0")/.."
feed_url=$(scripts/desktop-feed-url.sh 0.0.0-preview.1)

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT INT TERM
mkdir "$work/new" "$work/current"

$GH release download "$tag" --repo "$repo" --pattern appcast.xml --dir "$work/new" >/dev/null || die "$tag has no appcast.xml to publish"
[ -s "$work/new/appcast.xml" ] || die "$tag's appcast.xml is empty"

if $GH release view "$FEED_TAG" --repo "$repo" >/dev/null 2>&1; then
  # What the feed holds now. (A feed release with no asset yet is not an error: it is filled below.)
  $GH release download "$FEED_TAG" --repo "$repo" --pattern appcast.xml --dir "$work/current" >/dev/null 2>&1 || true
else
  [ -n "${GITHUB_SHA:-}" ] || die "GITHUB_SHA is not set: the feed release needs a commit to be tagged on"
  say "creating the $FEED_TAG release (a pre-release, never latest)"
  $GH release create "$FEED_TAG" --repo "$repo" --target "$GITHUB_SHA" --prerelease --latest=false \
    --title "Werkbord preview update feed" \
    --notes "Not a download. The update feed that Werkbord preview apps read: one file, appcast.xml, replaced by each verified release. Install previews from the release pages." >/dev/null ||
    die "could not create the $FEED_TAG release"
fi

if [ -s "$work/current/appcast.xml" ]; then
  current=$(sed -n 's#.*/releases/download/\(werkbord-v[^/"]*\)/.*#\1#p' "$work/current/appcast.xml" | head -n 1)
  if [ -n "$current" ] && [ "$current" != "$tag" ]; then
    rc=0
    scripts/semver-gt.sh "${tag#werkbord-v}" "${current#werkbord-v}" || rc=$?
    case $rc in
      0) ;;
      1) say "the feed already offers $current, which is newer than $tag: leaving it"; exit 0 ;;
      *) die "cannot compare $tag with the feed's $current" ;;
    esac
  fi
fi

$GH release upload "$FEED_TAG" "$work/new/appcast.xml" --repo "$repo" --clobber >/dev/null || die "could not upload the feed"
say "the preview feed now offers $tag"

# The public address, as an installed preview app reads it. The CDN can lag a moment behind an upload.
n=0
until WERKBORD_FEED_URL=$feed_url $VERIFY "$tag"; do
  n=$((n + 1))
  [ "$n" -lt 6 ] || die "$feed_url does not serve $tag's feed as an installed app needs it"
  say "not yet (try $n of 6); waiting ${RETRY_SLEEP}s"
  sleep "$RETRY_SLEEP"
done
say "ok  $feed_url serves $tag's feed"
