#!/bin/sh
# Build-time pins, never runtime overrides. fetch-rqlite rebuilds source from the pinned commit.
set -eu
cd "$(dirname "$0")/.."
premium=${PREMIUM_ISSUER_PUBLIC_KEY:-}
case "$premium" in *[!A-Za-z0-9_-]*) echo 'invalid Premium issuer public key encoding' >&2; exit 1 ;; esac
[ -z "$premium" ] || [ ${#premium} -eq 43 ] || { echo 'the Premium issuer public key must encode 32 Ed25519 bytes (43 characters)' >&2; exit 1; }
database=$(scripts/fetch-rqlite.sh)
network=$(scripts/fetch-nebula.sh)
sha() { if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d' ' -f1; else shasum -a 256 "$1" | cut -d' ' -f1; fi; }
printf '%s\n' "-X devboard/internal/entitlement.issuerPublicKey=$premium -X devboard/internal/team/infra/rqlite.distributionBinarySHA256=$(sha "$database/rqlited") -X devboard/internal/team/infra/nebula.distributionBinarySHA256=$(sha "$network/nebula")"
