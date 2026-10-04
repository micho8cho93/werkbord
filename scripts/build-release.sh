#!/bin/sh
# Builds the release archives and their checksums for one product.
#
#   scripts/build-release.sh <product> vX.Y.Z [outdir]
#
# <product> is werkbord or werkbord-team (see scripts/product.sh). One archive per
# platform, named <asset>_<version>_<os>_<arch>.tar.gz (.zip on Windows), each
# holding the executable and the README, and a checksums.txt in sha256sum format
# beside them. For werkbord the asset is "devboard", which is the layout the installer
# and `devboard update` read.
#
# The individual product's web app must already be built and embedded
# (make web web-embed). PLATFORMS overrides what is built, as "os/arch os/arch".
set -eu

PRODUCT=${1:-}
VERSION=${2:-}
OUT=${3:-dist}
case "$VERSION" in
v[0-9]*.[0-9]*.[0-9]*) ;;
*) echo "usage: $0 <werkbord|werkbord-team> vX.Y.Z [outdir]" >&2; exit 2 ;;
esac

cd "$(dirname "$0")/.."
CMD=$(scripts/product.sh "$PRODUCT" cmd)
BINARY=$(scripts/product.sh "$PRODUCT" binary)
ASSET=$(scripts/product.sh "$PRODUCT" asset)
README=$(scripts/product.sh "$PRODUCT" readme)
if [ "$PRODUCT" = werkbord ]; then
  [ -f internal/webui/dist/index.html ] || { echo "the web app is not built: run 'make web web-embed' first" >&2; exit 1; }
fi

PLATFORMS=${PLATFORMS:-"darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64 windows/arm64"}

rm -rf "$OUT"
mkdir -p "$OUT"
OUT=$(cd "$OUT" && pwd)
STAGE=$(mktemp -d)
trap 'rm -rf "$STAGE"' EXIT INT TERM

for p in $PLATFORMS; do
  os=${p%/*}
  arch=${p#*/}
  bin=$BINARY
  [ "$os" = windows ] && bin=$BINARY.exe
  dir="$STAGE/$os-$arch"
  mkdir -p "$dir"
  echo "building $PRODUCT $VERSION for $os/$arch"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$dir/$bin" "./$CMD"
  cp "$README" "$dir/README.md"
  name="${ASSET}_${VERSION#v}_${os}_${arch}"
  if [ "$os" = windows ]; then
    (cd "$dir" && zip -q "$OUT/$name.zip" "$bin" README.md)
  else
    tar -czf "$OUT/$name.tar.gz" -C "$dir" "$bin" README.md
  fi
done

cd "$OUT"
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum "${ASSET}"_* > checksums.txt
else
  shasum -a 256 "${ASSET}"_* > checksums.txt
fi
echo "wrote $(ls | wc -l | tr -d ' ') files to $OUT"
