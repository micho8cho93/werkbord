#!/bin/sh
# Verifies the release tag, the last step of every implementation commit
# (see AGENTS.md and docs/VERSIONING.md).
#
#   scripts/verify-tag.sh [tag [commit]]
#
# With no argument it checks the tag for the version in cmd/werkbord/VERSION (the one
# version of the whole release), against HEAD. It fails unless:
#   - the tag exists and is an annotated tag;
#   - it points at the commit (default HEAD);
#   - the VERSION file AT THAT COMMIT says the tag's version;
#   - the version is higher than every other release tag;
#   - the tag follows the release's naming (werkbord-vX.Y.Z).
set -eu

here=$(dirname "$0")
fail() { echo "verify-tag: $*" >&2; exit 1; }

product=werkbord
prefix=$("$here/product.sh" "$product" tag-prefix)
versionfile=$("$here/product.sh" "$product" version-file)
tag=${1:-$("$here/product.sh" "$product" tag)}
cd "$here/.."
commit=$(git rev-parse --verify "${2:-HEAD}^{commit}") || fail "no such commit ${2:-HEAD}"

case "$tag" in
  "$prefix"[0-9]*.[0-9]*.[0-9]*) ;;
  *) fail "\"$tag\" is not a release tag: want $prefix<MAJOR.MINOR.PATCH>" ;;
esac
want=${tag#"$prefix"}
git rev-parse -q --verify "refs/tags/$tag" >/dev/null || fail "the tag $tag does not exist (make tag)"
[ "$(git cat-file -t "refs/tags/$tag")" = tag ] || fail "$tag is not an annotated tag"
at=$(git rev-parse "refs/tags/$tag^{commit}")
[ "$at" = "$commit" ] || fail "$tag points at $(git rev-parse --short "$at"), not at $(git rev-parse --short "$commit")"
have=$(git show "$commit:$versionfile" 2>/dev/null | tr -d '[:space:]') || fail "$versionfile does not exist at $(git rev-parse --short "$commit")"
[ "$have" = "$want" ] || fail "$tag points at a commit whose $versionfile says $have, not $want"

# strictly newer than the other release tags
newest=$(git tag --list "${prefix}[0-9]*" | grep -v "^$tag\$" | sed "s/^$prefix//" | sort -t. -k1,1n -k2,2n -k3,3n | tail -1 || true)
if [ -n "$newest" ]; then
  top=$(printf '%s\n%s\n' "$newest" "$want" | sort -t. -k1,1n -k2,2n -k3,3n | tail -1)
  [ "$top" = "$want" ] || fail "$want is not higher than the existing $prefix$newest"
fi
echo "ok: $tag -> $(git rev-parse --short "$commit"), $versionfile is $have"
