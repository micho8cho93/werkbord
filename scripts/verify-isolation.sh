#!/bin/sh
# Proves the individual product does not need Team: copies the repository
# with every Team file removed, then builds, vets and tests what is left, and
# runs the executable.
#
#   scripts/verify-isolation.sh
#
# The import-graph tests in internal/archtest say the same thing statically; this
# is the empirical version, and it also catches a build file, script or embedded
# asset that reaches for something under Team. It needs go; it installs nothing and
# touches nothing outside a temporary directory.
set -eu

cd "$(dirname "$0")/.."
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT INT TERM

# (internal/update only names Team's release tags, to refuse them.)
# What belongs to Team and to nothing else (docs/PRODUCTS.md, "Where Team code goes").
TEAM_PATHS="internal/team cmd/werkbord-team"

echo "copying the repository without: $TEAM_PATHS"
mkdir "$WORK/repo"
# Tracked and not-ignored files only, with the Team paths dropped.
git ls-files --cached --others --exclude-standard | grep -v -e '^internal/team/' -e '^cmd/werkbord-team/' | tar -cf - -T - | tar -xf - -C "$WORK/repo"
for p in $TEAM_PATHS; do
  [ ! -e "$WORK/repo/$p" ] || { echo "verify-isolation: $p is still in the copy" >&2; exit 1; }
done

SRC=$(pwd)
cd "$WORK/repo"
# The web build is the individual product's and is embedded as-is; reuse the one here if it was built.
if [ -f "$SRC/internal/webui/dist/index.html" ]; then
  mkdir -p internal/webui/dist && cp -R "$SRC/internal/webui/dist/." internal/webui/dist/
fi

echo "go build ./..."
go build ./...
echo "go vet ./..."
go vet ./...
echo "go test ./... (the individual product and the shared packages, with Team absent)"
# (30 minutes, not Go's ten: the end-to-end tests build and run real programs, which a busy computer takes a while over)
if ! go test -timeout 30m ./... > "$WORK/test.log" 2>&1; then
  tail -n 60 "$WORK/test.log" >&2
  echo "verify-isolation: the tests fail with Team removed" >&2
  exit 1
fi
grep -c '^ok' "$WORK/test.log" | sed 's/$/ packages pass/' 

echo "running the executable"
go build -o "$WORK/werkbord" ./cmd/werkbord
"$WORK/werkbord" version
WERKBORD_DATA_DIR="$WORK/data" "$WORK/werkbord" migrate

if grep -rIl --exclude-dir=.git --exclude-dir=node_modules -e 'internal/team' -e 'werkbord-team' . | grep -v -e '^./docs/' -e '^./AGENTS.md' -e '^./scripts/' -e '^./Makefile' -e '^./.github/' -e '^./internal/archtest/' -e '^./internal/update/' -e '^./README.md' ; then
  echo "verify-isolation: the files above mention Team outside documentation and build tooling" >&2
  exit 1
fi
echo "ok: the individual product builds, tests and runs with Team removed"
