#!/bin/sh
# Verifies a product's release tag, the last step of every implementation commit
# (see AGENTS.md and docs/VERSIONING.md).
#
#   scripts/verify-tag.sh <product> [tag [commit]]
#
# With only a product it checks the tag for the version in that product's VERSION
# file, against HEAD. It fails unless:
#   - the tag exists and is an annotated tag;
#   - it points at the commit (default HEAD);
#   - the VERSION file AT THAT COMMIT says the tag's version;
#   - the version is higher than every other tag of the same product, and no other
#     product's tag points at the commit by mistake of name;
#   - the tag follows the product's naming (werkbord-vX.Y.Z / werkbord-team-vX.Y.Z).
set -eu

here=$(dirname "$0")
fail() { echo "verify-tag: $*" >&2; exit 1; }

product=${1:-}
[ -n "$product" ] || { echo "usage: $0 <product> [tag [commit]]" >&2; exit 2; }
prefix=$("$here/product.sh" "$product" tag-prefix)
cmd=$("$here/product.sh" "$product" cmd)
tag=${2:-$("$here/product.sh" "$product" tag)}
cd "$here/.."
commit=$(git rev-parse --verify "${3:-HEAD}^{commit}") || fail "no such commit ${3:-HEAD}"

case "$tag" in
  "$prefix"[0-9]*.[0-9]*.[0-9]*) ;;
  *) fail "\"$tag\" is not a $product tag: want $prefix<MAJOR.MINOR.PATCH>" ;;
esac
want=${tag#"$prefix"}
git rev-parse -q --verify "refs/tags/$tag" >/dev/null || fail "the tag $tag does not exist (make tag PRODUCT=$product)"
[ "$(git cat-file -t "refs/tags/$tag")" = tag ] || fail "$tag is not an annotated tag"
at=$(git rev-parse "refs/tags/$tag^{commit}")
[ "$at" = "$commit" ] || fail "$tag points at $(git rev-parse --short "$at"), not at $(git rev-parse --short "$commit")"
have=$(git show "$commit:$cmd/VERSION" 2>/dev/null | tr -d '[:space:]') || fail "$cmd/VERSION does not exist at $(git rev-parse --short "$commit")"
[ "$have" = "$want" ] || fail "$tag points at a commit whose $cmd/VERSION says $have, not $want"

# strictly newer than the product's other tags
newest=$(git tag --list "${prefix}[0-9]*" | grep -v "^$tag\$" | sed "s/^$prefix//" | sort -t. -k1,1n -k2,2n -k3,3n | tail -1 || true)
if [ -n "$newest" ]; then
  top=$(printf '%s\n%s\n' "$newest" "$want" | sort -t. -k1,1n -k2,2n -k3,3n | tail -1)
  [ "$top" = "$want" ] || fail "$want is not higher than the existing $prefix$newest"
fi
echo "ok: $tag -> $(git rev-parse --short "$commit"), $cmd/VERSION is $have"
