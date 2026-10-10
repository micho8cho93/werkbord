#!/bin/sh
# Describes what this repository releases, in one place. The Makefile, the release
# scripts and CI all read it, so an executable's name and the version and tag it is
# released under are written down once.
#
# There is ONE version and ONE release (docs/VERSIONING.md): cmd/werkbord/VERSION,
# the tag werkbord-vX.Y.Z, one Mac app. Werkbord and Werkbord Team are the two
# executables of that release; they differ in name and archive, never in version.
#
#   scripts/product.sh <executable> <field>
#   scripts/product.sh from-tag <tag>          check that a release tag is the release's
#
# Executables and fields:
#   werkbord        the controller and command line (cmd/werkbord)
#   werkbord-team   the Team service and command line (cmd/werkbord-team)
#
#   cmd             the directory of the executable's main package
#   binary          the executable's name
#   asset           the prefix of the release archives:   <asset>_<version>_<os>_<arch>.tar.gz
#   legacy-asset    an older name the same archives are also published under, for the
#                   updaters and installers of releases from before a rename (empty if none)
#   readme          the README packed into the archives
#   version-file    the one VERSION file, for both executables: cmd/werkbord/VERSION
#   tag-prefix      what every release tag starts with:    werkbord-v
#   version         the version in the VERSION file, without a "v": 4.0.0
#   tag             the tag of that version:               werkbord-v4.0.0
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
    werkbord-team-v[0-9]*) die "\"${2:-}\" is a retired tag series: Werkbord Team is released with Werkbord, under werkbord-vX.Y.Z" ;;
    werkbord-v[0-9]*.[0-9]*.[0-9]*) echo werkbord ;;
    *) die "\"${2:-}\" is not a release tag (werkbord-vX.Y.Z)" ;;
  esac
  exit 0
fi

product=${1:-}
field=${2:-}
case "$product" in
  werkbord)      cmd=cmd/werkbord;       binary=werkbord;      asset=werkbord;      legacy=devboard; readme=README.md ;;
  werkbord-team) cmd=cmd/werkbord-team;  binary=werkbord-team; asset=werkbord-team; legacy="";      readme=cmd/werkbord-team/README.md ;;
  *) die "unknown product \"$product\" (want werkbord or werkbord-team)" ;;
esac
# One version for everything that is released together.
versionfile=cmd/werkbord/VERSION
prefix="werkbord-v"

root=$(cd "$(dirname "$0")/.." && pwd)
version() {
  v=$(tr -d '[:space:]' < "$root/$versionfile" 2>/dev/null) || die "$versionfile is missing"
  case "$v" in
    [0-9]*.[0-9]*.[0-9]*) ;;
    *) die "$versionfile says \"$v\": want MAJOR.MINOR.PATCH" ;;
  esac
  printf '%s' "$v"
}

case "$field" in
  cmd) echo "$cmd" ;;
  binary) echo "$binary" ;;
  asset) echo "$asset" ;;
  legacy-asset) echo "$legacy" ;;
  readme) echo "$readme" ;;
  version-file) echo "$versionfile" ;;
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
