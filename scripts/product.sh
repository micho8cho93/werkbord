#!/bin/sh
# Describes the products this repository releases, in one place. The Makefile, the
# release scripts and CI all read it, so a product's name, version file and tag
# prefix are written down once.
#
#   scripts/product.sh <product> <field>
#   scripts/product.sh from-tag <tag>          which product a release tag belongs to
#
# Products and fields:
#   werkbord        the individual product  (cmd/devboard, executable "devboard")
#   werkbord-team   the Team product        (cmd/werkbord-team)
#
#   cmd             the directory of the product's main package; its VERSION file lives there
#   binary          the executable's name
#   asset           the prefix of the release archives:   <asset>_<version>_<os>_<arch>.tar.gz
#   readme          the README packed into the archives
#   tag-prefix      what every release tag starts with:    werkbord-v
#   version         the version in the VERSION file, without a "v": 0.8.0
#   tag             the tag of that version:               werkbord-v0.8.0
#   build-version   what a build from this working tree reports (see below)
#
# build-version is "v0.8.0" only for a clean checkout of the commit its release tag
# points at. Anything else (a commit past the tag, a dirty tree, a tag not made yet)
# reports v0.8.0-<commits since>-g<sha>[-dirty], which the updater recognises as a
# build from source and never treats as an installed release.
set -eu

die() { echo "product.sh: $*" >&2; exit 2; }

if [ "${1:-}" = from-tag ]; then
  case "${2:-}" in
    werkbord-team-v[0-9]*.[0-9]*.[0-9]*) echo werkbord-team ;;
    werkbord-v[0-9]*.[0-9]*.[0-9]*) echo werkbord ;;
    *) die "\"${2:-}\" is not a product release tag (werkbord-vX.Y.Z or werkbord-team-vX.Y.Z)" ;;
  esac
  exit 0
fi

product=${1:-}
field=${2:-}
case "$product" in
  werkbord)      cmd=cmd/devboard;       binary=devboard;      asset=devboard;      readme=README.md ;;
  werkbord-team) cmd=cmd/werkbord-team;  binary=werkbord-team; asset=werkbord-team; readme=cmd/werkbord-team/README.md ;;
  *) die "unknown product \"$product\" (want werkbord or werkbord-team)" ;;
esac
prefix="$product-v"

root=$(cd "$(dirname "$0")/.." && pwd)
version() {
  v=$(tr -d '[:space:]' < "$root/$cmd/VERSION" 2>/dev/null) || die "$cmd/VERSION is missing"
  case "$v" in
    [0-9]*.[0-9]*.[0-9]*) ;;
    *) die "$cmd/VERSION says \"$v\": want MAJOR.MINOR.PATCH" ;;
  esac
  printf '%s' "$v"
}

case "$field" in
  cmd) echo "$cmd" ;;
  binary) echo "$binary" ;;
  asset) echo "$asset" ;;
  readme) echo "$readme" ;;
  tag-prefix) echo "$prefix" ;;
  version) version; echo ;;
  tag) echo "$prefix$(version)" ;;
  build-version)
    v=$(version)
    tag="$prefix$v"
    if ! command -v git >/dev/null 2>&1 || ! git -C "$root" rev-parse --git-dir >/dev/null 2>&1; then
      echo dev
      exit 0
    fi
    sha=$(git -C "$root" rev-parse --short=7 HEAD 2>/dev/null || echo 0000000)
    dirty=""
    [ -n "$(git -C "$root" status --porcelain 2>/dev/null)" ] && dirty=-dirty
    n=0
    if git -C "$root" rev-parse -q --verify "refs/tags/$tag" >/dev/null 2>&1; then
      n=$(git -C "$root" rev-list --count "$tag..HEAD")
    fi
    if [ "$n" = 0 ] && [ -z "$dirty" ] && [ "$(git -C "$root" rev-parse -q --verify "refs/tags/$tag^{commit}" 2>/dev/null)" = "$(git -C "$root" rev-parse HEAD)" ]; then
      echo "v$v"
    else
      echo "v$v-$n-g$sha$dirty"
    fi
    ;;
  *) die "unknown field \"$field\"" ;;
esac
