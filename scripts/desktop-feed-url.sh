#!/bin/sh
# Prints the address the Mac app of a given version looks for updates at. One definition, used by the build, the check of a
# built app, the publication of the preview feed and their tests.
#
#   scripts/desktop-feed-url.sh 4.24.6              the STABLE feed: the appcast of the newest release marked "latest"
#   scripts/desktop-feed-url.sh 4.65.2-preview.1    the PREVIEW feed: the appcast of the newest release of any kind
#
# GitHub's /releases/latest never points at a pre-release, and every preview is one, so an app built from a preview could never
# find its next version at the stable address. A preview follows the preview feed instead: a release called
# werkbord-preview-feed (itself a pre-release, never latest) that holds exactly one asset, appcast.xml, which release.yml replaces
# with each published release's own appcast once that release has been verified (scripts/publish-preview-feed.sh). A stable app
# keeps following the stable address, so a preview can never be offered to it.
set -eu

REPO=micho8cho93/werkbord
PREVIEW_FEED_TAG=werkbord-preview-feed

[ "$#" -eq 1 ] || { echo "usage: desktop-feed-url.sh <version>" >&2; exit 2; }
version=${1#werkbord-v}
version=${version#v}
case "$version" in
  [0-9]*.[0-9]*.[0-9]*-*) echo "https://github.com/$REPO/releases/download/$PREVIEW_FEED_TAG/appcast.xml" ;;
  [0-9]*.[0-9]*.[0-9]*) echo "https://github.com/$REPO/releases/latest/download/appcast.xml" ;;
  *) echo "desktop-feed-url: $1 is not a version" >&2; exit 2 ;;
esac
