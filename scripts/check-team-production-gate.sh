#!/bin/sh
# Stable unified releases include Team's local service, so require the reviewed production gate to approve this exact build.
set -eu

die() { printf 'team production release gate: %s\n' "$*" >&2; exit 1; }
[ "$#" -eq 1 ] || die "usage: scripts/check-team-production-gate.sh <stable-version>"
version=$1
printf '%s' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' || die "version must be stable semantic version"
approval="Production admission: APPROVED for v$version"
if ! grep -Fqx "$approval" docs/TEAM_SECURITY_GATE.md; then
  die "docs/TEAM_SECURITY_GATE.md does not contain '$approval'; resolve the listed acceptance gaps or record a reviewed, version-specific exception before publication"
fi
printf 'team production release gate: v%s is approved\n' "$version"
