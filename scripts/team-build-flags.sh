#!/bin/sh
# Build-time pins, never runtime overrides. fetch-rqlite rebuilds source from the pinned commit.
set -eu
cd "$(dirname "$0")/.."
issuer=${LICENSE_ISSUER_PUBLIC_KEY:-}
case "$issuer" in *[!A-Za-z0-9_-]*) echo 'invalid license public key encoding' >&2; exit 1 ;; esac
[ -z "$issuer" ] || [ ${#issuer} -eq 43 ] || { echo 'license public key must be 32 bytes in raw URL base64' >&2; exit 1; }
pin=""
if [ "$(uname -s)" = Darwin ]; then
  dir=$(scripts/fetch-rqlite.sh)
  pin=$(shasum -a 256 "$dir/rqlited" | cut -d' ' -f1)
fi
printf '%s\n' "-X main.licenseIssuer=$issuer -X devboard/internal/team/infra/rqlite.distributionBinarySHA256=$pin"
