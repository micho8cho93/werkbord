#!/bin/sh
# Builds the release archives and their checksums for one product.
#
#   scripts/build-release.sh <product> vX.Y.Z [outdir]
#
# <product> is werkbord or werkbord-team (see scripts/product.sh). One archive per
# platform, named <asset>_<version>_<os>_<arch>.tar.gz (.zip on Windows), each
# holding the executable and the README, and a checksums.txt in sha256sum format
# beside them.
#
# A product that was renamed (werkbord was "devboard") also publishes every archive under
# its old name, with the executable inside under its old name: that is what the installers
# and `devboard update` of the releases from before the rename look for, so they upgrade
# to this one.
#
# The individual product's web app must already be built and embedded
# (make web web-embed). PLATFORMS overrides what is built, as "os/arch os/arch".
#
# Werkbord Team's archives for macOS and Linux also carry the pinned Nebula release it supervises
# (libexec/werkbord-team/nebula, checked against internal/team/infra/nebula/manifest.go by
# scripts/fetch-nebula.sh) and the licences that go with it (licenses/). The program is never downloaded
# when Team runs. BUNDLE_NEBULA=no leaves it out, for tests of the archive format that must not touch the network.
#
# They carry the pinned rqlite release too (libexec/werkbord-team/rqlited, checked against
# internal/team/infra/rqlite/manifest.go by scripts/fetch-rqlite.sh), for Linux. The project publishes no binary for macOS, and
# a program with cgo is built from the pinned source on a Mac only, so a macOS archive made anywhere else goes without it and
# says so (such a host keeps its workspace in one file, or builds the program with `make rqlite`). BUNDLE_RQLITE=no leaves it out.
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
LEGACY=$(scripts/product.sh "$PRODUCT" legacy-asset)
README=$(scripts/product.sh "$PRODUCT" readme)
if [ "$PRODUCT" = werkbord ]; then
  [ -f internal/webui/dist/index.html ] || { echo "the web app is not built: run 'make web web-embed' first" >&2; exit 1; }
fi

BUNDLE_NEBULA=${BUNDLE_NEBULA:-yes}
BUNDLE_RQLITE=${BUNDLE_RQLITE:-yes}
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
  members="$bin README.md"
  if [ "$PRODUCT" = werkbord-team ] && [ "$os" != windows ] && [ "$BUNDLE_NEBULA" != no ]; then
    nebula_dir=$(scripts/fetch-nebula.sh "$os" "$arch") || { echo "build-release: cannot get the pinned Nebula for $os/$arch" >&2; exit 1; }
    mkdir -p "$dir/libexec/werkbord-team" "$dir/licenses/nebula" "$dir/licenses/go"
    cp "$nebula_dir/nebula" "$dir/libexec/werkbord-team/nebula"
    chmod 755 "$dir/libexec/werkbord-team/nebula"
    cp third_party/nebula/LICENSE third_party/nebula/THIRD_PARTY_LICENSES.txt "$dir/licenses/nebula/"
    cp third_party/go/LICENSE "$dir/licenses/go/LICENSE"
    members="$members libexec licenses"
    if [ "$BUNDLE_RQLITE" != no ]; then
      rc=0
      rqlite_dir=$(scripts/fetch-rqlite.sh "$os" "$arch") || rc=$?
      case $rc in
        0)
          mkdir -p "$dir/licenses/rqlite"
          cp "$rqlite_dir/rqlited" "$dir/libexec/werkbord-team/rqlited"
          chmod 755 "$dir/libexec/werkbord-team/rqlited"
          [ ! -f "$rqlite_dir/rqlited.build" ] || cp "$rqlite_dir/rqlited.build" "$dir/libexec/werkbord-team/rqlited.build"
          cp third_party/rqlite/LICENSE third_party/rqlite/THIRD_PARTY_LICENSES.txt "$dir/licenses/rqlite/" ;;
        3) echo "build-release: NOTE: the $os/$arch archive carries no database program (rqlite is built from source, on $os only): a Workspace Host on it keeps its data in one file, or builds the program with 'make rqlite'" >&2 ;;
        *) echo "build-release: cannot get the pinned rqlite for $os/$arch" >&2; exit 1 ;;
      esac
    fi
  fi
  name="${ASSET}_${VERSION#v}_${os}_${arch}"
  if [ "$os" = windows ]; then
    (cd "$dir" && zip -q "$OUT/$name.zip" "$bin" README.md)
  else
    # shellcheck disable=SC2086 # the members have no spaces
    tar -czf "$OUT/$name.tar.gz" -C "$dir" $members
  fi
  if [ -n "$LEGACY" ]; then
    old="$STAGE/$os-$arch-legacy"
    mkdir -p "$old"
    oldbin=$LEGACY
    [ "$os" = windows ] && oldbin=$LEGACY.exe
    cp "$dir/$bin" "$old/$oldbin"
    cp "$README" "$old/README.md"
    oldname="${LEGACY}_${VERSION#v}_${os}_${arch}"
    if [ "$os" = windows ]; then
      (cd "$old" && zip -q "$OUT/$oldname.zip" "$oldbin" README.md)
    else
      tar -czf "$OUT/$oldname.tar.gz" -C "$old" "$oldbin" README.md
    fi
  fi
done

cd "$OUT"
set -- "${ASSET}"_*
[ -n "$LEGACY" ] && set -- "$@" "${LEGACY}"_*
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum "$@" > checksums.txt
else
  shasum -a 256 "$@" > checksums.txt
fi
echo "wrote $(ls | wc -l | tr -d ' ') files to $OUT"
