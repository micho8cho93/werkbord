#!/bin/sh
# Tests the preview update feed: which address each kind of app looks at (scripts/desktop-feed-url.sh), and the publication of a
# release's appcast as the preview feed (scripts/publish-preview-feed.sh) against a fake GitHub: the feed release is created once
# and never made "latest", a newer release replaces it, an older or repeated one cannot move it backwards, and a feed that the public
# address does not serve is a failure. It needs no network and no Mac, and runs in a second.
#
#   scripts/test-preview-feed.sh
set -eu
cd "$(dirname "$0")/.."
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT INT TERM
pass=0
ok() { pass=$((pass + 1)); printf '  ok  %s\n' "$1"; }
bad() { printf 'FAIL  %s\n' "$1" >&2; [ -n "${2:-}" ] && printf '%s\n' "$2" >&2; exit 1; }
contains() { case "$1" in *"$2"*) return 0 ;; esac; return 1; }

STABLE=https://github.com/micho8cho93/werkbord/releases/latest/download/appcast.xml
PREVIEW=https://github.com/micho8cho93/werkbord/releases/download/werkbord-preview-feed/appcast.xml

# ---- which feed an app looks at ----
for v in 4.24.6 v4.24.6 4.0.0 12.1.3; do
  [ "$(scripts/desktop-feed-url.sh "$v")" = "$STABLE" ] || bad "$v does not look at the stable feed"
done
ok "a stable app looks at the stable feed"
for v in 4.65.2-preview.1 v4.65.2-preview.1 werkbord-v4.65.2-preview.1 4.66.0-preview.12 5.0.0-rc.1; do
  [ "$(scripts/desktop-feed-url.sh "$v")" = "$PREVIEW" ] || bad "$v does not look at the preview feed"
done
ok "a preview app looks at the preview feed, which is never the address /releases/latest follows"
for v in dev "" 4.24 nonsense; do
  if scripts/desktop-feed-url.sh "$v" >/dev/null 2>&1; then bad "'$v' was given a feed"; fi
done
ok "something that is not a version is given no feed"

# ---- a fake GitHub ----
FAKE="$WORK/github"
cat > "$WORK/gh" <<'GH'
#!/bin/sh
# Just enough of the GitHub CLI: release view / download / create / upload, against the directory $FAKE.
set -eu
echo "$*" >> "$FAKE/log"
[ "$1" = release ] || { echo "fake gh: $*" >&2; exit 9; }
sub=$2; tag=$3; shift 3
dir= clobber= latest=
while [ $# -gt 0 ]; do
  case "$1" in
    --dir) dir=$2; shift 2 ;;
    --clobber) clobber=1; shift ;;
    --latest=*) latest=$1; shift ;;
    --repo|--pattern|--target|--title|--notes) shift 2 ;;
    --prerelease) shift ;;
    *) file=$1; shift ;;
  esac
done
case "$sub" in
  view) [ -d "$FAKE/releases/$tag" ] ;;
  download) [ -s "$FAKE/releases/$tag/appcast.xml" ] || exit 1; cp "$FAKE/releases/$tag/appcast.xml" "$dir/appcast.xml" ;;
  create) [ "$latest" = --latest=false ] || { echo "the feed release was not created with --latest=false" >&2; exit 8; }; mkdir -p "$FAKE/releases/$tag" ;;
  upload) [ -n "$clobber" ] || exit 7; cp "$file" "$FAKE/releases/$tag/appcast.xml" ;;
  *) exit 9 ;;
esac
GH
chmod +x "$WORK/gh"
cat > "$WORK/verify" <<'V'
#!/bin/sh
# Stands for scripts/verify-appcast.sh: records the feed address it was given and passes unless told otherwise.
echo "verify $1 feed=$WERKBORD_FEED_URL" >> "$FAKE/log"
[ ! -f "$FAKE/verify-fails" ]
V
chmod +x "$WORK/verify"

# release <tag>: a published release with an appcast whose item points at that tag's archive
release() { mkdir -p "$FAKE/releases/$1"; printf '<rss><item><enclosure url="https://github.com/micho8cho93/werkbord/releases/download/%s/Werkbord_x.zip"/></item></rss>\n' "$1" > "$FAKE/releases/$1/appcast.xml"; }
reset() { rm -rf "$FAKE"; mkdir -p "$FAKE"; : > "$FAKE/log"; }
publish() { GH="$WORK/gh" VERIFY="$WORK/verify" RETRY_SLEEP=0 GITHUB_REPOSITORY=micho8cho93/werkbord GITHUB_SHA=abc123 FAKE="$FAKE" scripts/publish-preview-feed.sh "$1"; }
fed() { cat "$FAKE/releases/werkbord-preview-feed/appcast.xml" 2>/dev/null || true; }

# the first preview creates the feed release (a pre-release that is not latest) and fills it with the release's own appcast
reset; release werkbord-v4.66.0-preview.1
publish werkbord-v4.66.0-preview.1 >/dev/null 2>&1 || bad "the first publication failed"
[ "$(fed)" = "$(cat "$FAKE/releases/werkbord-v4.66.0-preview.1/appcast.xml")" ] || bad "the feed is not the release's appcast, byte for byte"
[ "$(grep -c '^release create' "$FAKE/log")" = 1 ] || bad "the feed release was not created exactly once"
contains "$(cat "$FAKE/log")" "feed=$PREVIEW" || bad "the public check was not made at the preview feed's address" "$(cat "$FAKE/log")"
ok "the first release creates the feed (never latest), fills it with its own appcast and checks the public address"

# a newer release replaces it, and the feed release is not created again
release werkbord-v4.66.0-preview.2
publish werkbord-v4.66.0-preview.2 >/dev/null 2>&1 || bad "a newer preview was not published"
contains "$(fed)" "werkbord-v4.66.0-preview.2/" && ! contains "$(fed)" "preview.1/" || bad "the feed does not offer the newer preview alone" "$(fed)"
[ "$(grep -c '^release create' "$FAKE/log")" = 1 ] || bad "the feed release was created again"
ok "a newer release replaces the feed, which holds that release alone"

# a stable release moves a preview app on to it
release werkbord-v4.66.0
publish werkbord-v4.66.0 >/dev/null 2>&1 || bad "a stable release was not published to the preview feed"
contains "$(fed)" "werkbord-v4.66.0/" || bad "a preview app would not be carried to the stable release" "$(fed)"
ok "a stable release is published to the preview feed too, so a preview app is carried to it"

# an older release, repeated later, cannot move the feed backwards
release werkbord-v4.65.2-preview.1
publish werkbord-v4.65.2-preview.1 >"$WORK/out" 2>&1 || bad "an older release made the publication fail instead of being left alone" "$(cat "$WORK/out")"
contains "$(fed)" "werkbord-v4.66.0/" || bad "an older release moved the feed backwards" "$(fed)"
contains "$(cat "$WORK/out")" "leaving it" || bad "the older release was not reported as left alone" "$(cat "$WORK/out")"
ok "an older release cannot move the feed backwards"

# the same release again (a re-run) is fine and checks again
before=$(grep -c '^verify' "$FAKE/log")
publish werkbord-v4.66.0 >/dev/null 2>&1 || bad "a re-run of the newest release failed"
[ "$(grep -c '^verify' "$FAKE/log")" -gt "$before" ] || bad "a re-run did not check the public address"
ok "publishing the release the feed already offers again is harmless"

# a feed the public address does not serve is a failure, after trying again
reset; release werkbord-v4.66.0-preview.1; : > "$FAKE/verify-fails"
if publish werkbord-v4.66.0-preview.1 >"$WORK/out" 2>&1; then bad "a feed that did not verify was reported as published" "$(cat "$WORK/out")"; fi
contains "$(cat "$WORK/out")" "does not serve werkbord-v4.66.0-preview.1" || bad "the failure does not say what is wrong" "$(cat "$WORK/out")"
[ "$(grep -c '^verify' "$FAKE/log")" = 6 ] || bad "the public check was not tried 6 times" "$(cat "$FAKE/log")"
ok "a feed the public address does not serve fails the release job, after 6 tries"

# things that are not a release of the individual product
reset
for t in werkbord-team-v1.0.0 v4.66.0 werkbord-v4 werkbord-preview-feed ""; do
  if publish "$t" >/dev/null 2>&1; then bad "'$t' was published as a feed"; fi
done
[ ! -d "$FAKE/releases/werkbord-preview-feed" ] || bad "a refused tag still created the feed release"
ok "only an individual release's tag can be published as the feed"

echo "ok: $pass checks"
