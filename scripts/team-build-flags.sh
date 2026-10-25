#!/bin/sh
# Build-time pins, never runtime overrides. fetch-rqlite rebuilds source from the pinned commit.
set -eu
cd "$(dirname "$0")/.."
issuer=${LICENSE_ISSUER_PUBLIC_KEY:-}
case "$issuer" in *[!A-Za-z0-9_-]*) echo 'invalid license public key encoding' >&2; exit 1 ;; esac
[ -z "$issuer" ] || [ ${#issuer} -eq 43 ] || { echo 'license public key must be 32 bytes in raw URL base64' >&2; exit 1; }
database=$(scripts/fetch-rqlite.sh)
network=$(scripts/fetch-nebula.sh)
sha() { if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d' ' -f1; else shasum -a 256 "$1" | cut -d' ' -f1; fi; }
printf '%s\n' "-X main.licenseIssuer=$issuer -X devboard/internal/team/infra/rqlite.distributionBinarySHA256=$(sha "$database/rqlited") -X devboard/internal/team/infra/nebula.distributionBinarySHA256=$(sha "$network/nebula")"
