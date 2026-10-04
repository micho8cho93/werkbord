#!/bin/sh
# Builds the release archives and their checksums.
#
#   scripts/build-release.sh v1.2.3 [outdir]
#
# One archive per platform, named devboard_<version>_<os>_<arch>.tar.gz (.zip on
# Windows), each holding the devboard executable and the README, and a
# checksums.txt in sha256sum format beside them. That is the layout the installer
# and `devboard update` read.
#
# The web app must already be built and embedded (make web web-embed).
# PLATFORMS overrides what is built, as "os/arch os/arch".
set -eu

VERSION=${1:-}
OUT=${2:-dist}
case "$VERSION" in
v[0-9]*.[0-9]*.[0-9]*) ;;
*) echo "usage: $0 vX.Y.Z [outdir]" >&2; exit 2 ;;
esac

cd "$(dirname "$0")/.."
[ -f internal/webui/dist/index.html ] || { echo "the web app is not built: run 'make web web-embed' first" >&2; exit 1; }

PLATFORMS=${PLATFORMS:-"darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64 windows/arm64"}

rm -rf "$OUT"
mkdir -p "$OUT"
OUT=$(cd "$OUT" && pwd)
STAGE=$(mktemp -d)
trap 'rm -rf "$STAGE"' EXIT INT TERM

for p in $PLATFORMS; do
  os=${p%/*}
  arch=${p#*/}
  bin=devboard
  [ "$os" = windows ] && bin=devboard.exe
  dir="$STAGE/$os-$arch"
  mkdir -p "$dir"
  echo "building $os/$arch"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$dir/$bin" ./cmd/devboard
  cp README.md "$dir/README.md"
  name="devboard_${VERSION#v}_${os}_${arch}"
  if [ "$os" = windows ]; then
    (cd "$dir" && zip -q "$OUT/$name.zip" "$bin" README.md)
  else
    tar -czf "$OUT/$name.tar.gz" -C "$dir" "$bin" README.md
  fi
done

cd "$OUT"
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum devboard_* > checksums.txt
else
  shasum -a 256 devboard_* > checksums.txt
fi
echo "wrote $(ls | wc -l | tr -d ' ') files to $OUT"
