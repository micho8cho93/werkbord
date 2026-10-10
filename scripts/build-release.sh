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
# says so (that archive is not a production Workspace Host distribution). BUNDLE_RQLITE=no is evaluation only.
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

if [ "$PRODUCT" = werkbord-team ] && [ "$BUNDLE_NEBULA" != no ]; then
  [ -n "${LICENSE_ISSUER_PUBLIC_KEY:-}" ] || { echo "Team distribution requires LICENSE_ISSUER_PUBLIC_KEY" >&2; exit 1; }
  [ -n "${WERKBORD_TEAM_RELEASE_SIGNING_KEY_FILE:-}" ] || { echo "Team distribution requires an offline release signing key file" >&2; exit 1; }
fi
if command -v sha256sum >/dev/null 2>&1; then
  sidecar_sha() { sha256sum "$1" | cut -d' ' -f1; }
else
  sidecar_sha() { shasum -a 256 "$1" | cut -d' ' -f1; }
fi

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
  database_pin=""
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
          database_pin=$(sidecar_sha "$rqlite_dir/rqlited")
          cp "$rqlite_dir/rqlited" "$dir/libexec/werkbord-team/rqlited"
          chmod 755 "$dir/libexec/werkbord-team/rqlited"
          [ ! -f "$rqlite_dir/rqlited.build" ] || cp "$rqlite_dir/rqlited.build" "$dir/libexec/werkbord-team/rqlited.build"
          cp third_party/rqlite/LICENSE third_party/rqlite/THIRD_PARTY_LICENSES.txt "$dir/licenses/rqlite/" ;;
        3) echo "build-release: NOTE: the $os/$arch archive carries no database program (rqlite is built from source, on $os only): not a production Workspace Host distribution; rebuild the complete bundle on a Mac" >&2 ;;
        *) echo "build-release: cannot get the pinned rqlite for $os/$arch" >&2; exit 1 ;;
      esac
    fi
  fi
  extra_flags=""
  cgo=0
  if [ "$PRODUCT" = werkbord-team ]; then
    extra_flags="-X main.licenseIssuer=${LICENSE_ISSUER_PUBLIC_KEY:-} -X devboard/internal/team/infra/rqlite.distributionBinarySHA256=$database_pin"
    if [ "$os" = darwin ] && [ "$(uname -s)" = Darwin ]; then
      cgo=1
      case "$arch" in arm64) c_arch=arm64 ;; amd64) c_arch=x86_64 ;; esac
      export CGO_CFLAGS="-arch $c_arch -mmacosx-version-min=13.0" CGO_LDFLAGS="-arch $c_arch -mmacosx-version-min=13.0"
    fi
  fi
  CGO_ENABLED=$cgo GOOS=$os GOARCH=$arch go build -trimpath -ldflags "-s -w -X main.version=$VERSION $extra_flags" -o "$dir/$bin" "./$CMD"
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
# Both executables are archived into the one release, so each has its own list: checksums.txt is Werkbord's (what the
# installer and `werkbord update` read), checksums-team.txt is Team's.
LIST=checksums.txt
[ "$PRODUCT" != werkbord-team ] || LIST=checksums-team.txt
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum "$@" > "$LIST"
else
  shasum -a 256 "$@" > "$LIST"
fi
# Team's archives are signed offline (the release key never touches CI) and the signature names the one release tag.
if [ "$PRODUCT" = werkbord-team ] && [ -n "${WERKBORD_TEAM_RELEASE_SIGNING_KEY_FILE:-}" ]; then
  cd - >/dev/null
  go run ./cmd/werkbord-team/vendor release --input "$OUT/$LIST" --out "$OUT/$LIST.sig" --tag "werkbord-$VERSION" < "$WERKBORD_TEAM_RELEASE_SIGNING_KEY_FILE"
fi
echo "wrote release files to $OUT"
