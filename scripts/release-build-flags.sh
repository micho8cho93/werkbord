#!/bin/sh
# Public build stamp only. Never discover a trust anchor from the download server.
set -eu
key=${RELEASE_PUBLIC_KEY:-}
case "$key" in *[!A-Za-z0-9_-]*) echo 'invalid release public key encoding' >&2; exit 1 ;; esac
[ -z "$key" ] || [ ${#key} -eq 43 ] || { echo 'release public key must encode 32 Ed25519 bytes' >&2; exit 1; }
printf '%s\n' "-X devboard/internal/releaseverify.PublicKey=$key"
