#!/bin/sh
# Makes the pinned rqlite release available to a Werkbord Team build, and prints the directory that holds the
# `rqlited` program for one platform. The version, the commit, the archive names and every SHA-256 are pinned in
# internal/team/infra/rqlite/manifest.go (the same file the running server checks the program against): what is
# downloaded is checked against the archive's pin before anything is unpacked, and the unpacked program against its
# own pin, and anything that does not match is refused. "Latest" is never fetched.
#
# The project publishes Linux (and Windows) binaries, and none for macOS. For a platform whose manifest entry is
# `Source: true` this script builds rqlited from the pinned source instead: it clones the release's tag, refuses it
# unless the commit is the pinned one, and builds with the version flags the project's own release build uses, so the
# program reports exactly the release's version and commit. A build is not reproducible across toolchains, so the
# service build embeds the fresh executable's hash. The record beside it (rqlited.build) is diagnostic only:
# replacing both executable and record cannot authorize execution.
#
#   scripts/fetch-rqlite.sh                 prints .cache/rqlite/<version>/<os>_<arch> for this computer
#   scripts/fetch-rqlite.sh linux arm64     the same for another platform (a release build; only where it is published)
#
# RQLITE_DIR=…   names a directory that already holds `rqlited` (offline builds, tests); it is still checked
#                against the upstream binary pin, and used as it is. Not supported for source-only platforms.
# RQLITE_CACHE=… changes where downloads and builds are kept (default .cache/rqlite, which git ignores).
set -eu

die() { printf 'fetch-rqlite: %s\n' "$*" >&2; exit 1; }
cd "$(dirname "$0")/.."
ROOT=$(pwd)
MANIFEST=internal/team/infra/rqlite/manifest.go

if command -v sha256sum >/dev/null 2>&1; then sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
else die "sha256sum or shasum is needed"; fi

os=${1:-$(uname -s | tr '[:upper:]' '[:lower:]')}
arch=${2:-$(uname -m)}
case "$arch" in x86_64) arch=amd64 ;; aarch64) arch=arm64 ;; esac

version=$(sed -n 's/^const Version = "\(.*\)"$/\1/p' "$MANIFEST")
commit=$(sed -n 's/^const SourceCommit = "\(.*\)"$/\1/p' "$MANIFEST")
[ -n "$version" ] && [ -n "$commit" ] || die "$MANIFEST is not what this script expects"
tag="v$version"
release="https://github.com/rqlite/rqlite/releases/download/$tag/"
line=$(grep -E "^[[:space:]]*\"$os/$arch\":" "$MANIFEST" || true)
[ -n "$line" ] || die "no pinned rqlite release for $os/$arch (see $MANIFEST)"
field() { printf '%s' "$line" | sed -n "s/.*$1: \"\\([^\"]*\\)\".*/\\1/p"; }
archive=$(field Archive); archive_sha=$(field ArchiveSHA256); binary_sha=$(field BinarySHA256)
source_build=no; case "$line" in *"Source: true"*) source_build=yes ;; esac

check_binary() {
  [ -f "$1/rqlited" ] || return 1
  if [ "$source_build" = yes ]; then
    # A record beside a cached executable can be forged with it. Only a fresh source build is trusted.
    [ "${fresh_source:-no}" = yes ]
  else
    [ "$(sha256 "$1/rqlited")" = "$binary_sha" ]
  fi
}

if [ -n "${RQLITE_DIR:-}" ]; then
  check_binary "$RQLITE_DIR" || die "$RQLITE_DIR/rqlited is not the pinned rqlite $version for $os/$arch"
  printf '%s\n' "$RQLITE_DIR"; exit 0
fi

dir="${RQLITE_CACHE:-$ROOT/.cache/rqlite}/$version/${os}_${arch}"
if check_binary "$dir"; then printf '%s\n' "$dir"; exit 0; fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM
mkdir -p "$tmp/x"

if [ "$source_build" = yes ]; then
  command -v git >/dev/null 2>&1 || die "git is needed to build rqlite from its pinned source"
  command -v go >/dev/null 2>&1 || die "the Go toolchain is needed to build rqlite from its pinned source"
  cross_mac=no
  if [ "$os" = darwin ] && [ "$(go env GOOS)" = darwin ]; then cross_mac=yes; fi
  if [ "$os/$arch" != "$(go env GOOS)/$(go env GOARCH)" ] && [ "$cross_mac" != yes ]; then
    # Exit 3 says "cannot be had here", as opposed to "was had and is wrong" (1): a release build on another platform may go without.
    printf 'fetch-rqlite: rqlite for %s/%s is built from source, and can only be built on that platform (this is %s/%s)\n' "$os" "$arch" "$(go env GOOS)" "$(go env GOARCH)" >&2
    exit 3
  fi
  echo "fetch-rqlite: building rqlite $version from source for $os/$arch (the project publishes no binary for it)" >&2
  git -c advice.detachedHead=false clone -q --depth 1 --branch "$tag" https://github.com/rqlite/rqlite.git "$tmp/src" || die "could not fetch rqlite $tag"
  [ "$(git -C "$tmp/src" rev-parse HEAD)" = "$commit" ] || die "tag $tag is at $(git -C "$tmp/src" rev-parse HEAD), but the pin is $commit: not using it"
  pkg="github.com/rqlite/rqlite/v${version%%.*}/cmd"
  if [ "$cross_mac" = yes ]; then
    case "$arch" in arm64) c_arch=arm64 ;; amd64) c_arch=x86_64 ;; *) die "unsupported Mac architecture" ;; esac
    (cd "$tmp/src" && CGO_ENABLED=1 GOOS=darwin GOARCH=$arch \
      CGO_CFLAGS="-arch $c_arch -mmacosx-version-min=13.0" CGO_LDFLAGS="-arch $c_arch -mmacosx-version-min=13.0" \
      go build -trimpath -ldflags "-w -s -X $pkg.Version=$tag -X $pkg.Commit=$commit" -o "$tmp/x/rqlited" ./cmd/rqlited) || die "the Mac build failed"
  else
    (cd "$tmp/src" && CGO_ENABLED=1 go build -trimpath -ldflags "-w -s -X $pkg.Version=$tag -X $pkg.Commit=$commit" -o "$tmp/x/rqlited" ./cmd/rqlited) || die "the build failed"
  fi
  chmod 0755 "$tmp/x/rqlited"
  fresh_source=yes
  if [ "$arch" = "$(go env GOARCH)" ]; then
    got=$("$tmp/x/rqlited" -version 2>&1 | grep -m1 "^rqlited")
    case "$got" in "rqlited $tag "*) ;; *) die "the program built reports '$got', not $tag" ;; esac
  fi
  printf '{"version":"%s","commit":"%s","sha256":"%s"}\n' "$tag" "$commit" "$(sha256 "$tmp/x/rqlited")" > "$tmp/x/rqlited.build"
else
  command -v curl >/dev/null 2>&1 || die "curl is needed to fetch rqlite"
  echo "fetch-rqlite: downloading rqlite $version for $os/$arch" >&2
  curl -fsSL --retry 3 --retry-delay 5 -o "$tmp/$archive" "${release}$archive" || die "could not download $archive"
  [ "$(sha256 "$tmp/$archive")" = "$archive_sha" ] || die "the download's SHA-256 is $(sha256 "$tmp/$archive"), but the pin is $archive_sha: not using it"
  top="${archive%.tar.gz}"
  tar -xzf "$tmp/$archive" -C "$tmp/x" "$top/rqlited" || die "$archive does not hold rqlited"
  mv "$tmp/x/$top/rqlited" "$tmp/x/rqlited"
  rmdir "$tmp/x/$top"
  chmod 0755 "$tmp/x/rqlited"
  check_binary "$tmp/x" || die "the program in $archive is not the pinned one"
fi

check_binary "$tmp/x" || die "the program is not the pinned one"
mkdir -p "$(dirname "$dir")"
rm -rf "$dir.partial"
mv "$tmp/x" "$dir.partial"
rm -rf "$dir"
mv "$dir.partial" "$dir"
printf '%s\n' "$dir"
