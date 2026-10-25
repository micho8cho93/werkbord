#!/bin/sh
# Public build stamp only. Never discover a trust anchor from the download server.
set -eu
key=${RELEASE_PUBLIC_KEY:-}
case "$key" in *[!A-Za-z0-9_-]*) echo 'invalid release public key encoding' >&2; exit 1 ;; esac
[ -z "$key" ] || [ ${#key} -eq 43 ] || { echo 'release public key must encode 32 Ed25519 bytes' >&2; exit 1; }
# The public key that checks Premium unlock keys. A build without it can check none, so everyone on it is on the Free plan.
# The private half is never in a build or in CI: it stays with the person who sells Premium (cmd/werkbord-team/vendor keygen).
premium=${PREMIUM_ISSUER_PUBLIC_KEY:-}
case "$premium" in *[!A-Za-z0-9_-]*) echo 'invalid Premium issuer public key encoding' >&2; exit 1 ;; esac
[ -z "$premium" ] || [ ${#premium} -eq 43 ] || { echo 'the Premium issuer public key must encode 32 Ed25519 bytes (43 characters)' >&2; exit 1; }
printf '%s\n' "-X devboard/internal/releaseverify.PublicKey=$key -X devboard/internal/entitlement.issuerPublicKey=$premium"
