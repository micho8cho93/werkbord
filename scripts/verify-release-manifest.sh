#!/bin/sh
# Run only a trusted copy of this verifier with an independently provisioned PEM.
set -eu
die() { printf 'release verification: %s\n' "$*" >&2; exit 1; }
[ "$#" -eq 4 ] || die 'usage: verify-release-manifest.sh <tag> <manifest> <signature> <trusted-public.pem>'
tag=$1; manifest=$2; signature=$3; key=$4
printf '%s' "$tag" | grep -Eq '^(werkbord-v|v)[0-9]+\.[0-9]+\.[0-9]+(-preview\.[1-9][0-9]*)?$' || die 'invalid release tag'
[ -f "$key" ] || die 'independently trusted release key is required'
[ "$(wc -c < "$manifest")" -le 1048576 ] || die 'manifest is too large'
[ "$(wc -c < "$signature")" -eq 64 ] || die 'invalid signature length'
awk 'NF!=2 || length($1)!=64 || $1~/[^0-9a-fA-F]/ {exit 1}
 {name=$2; sub(/^\*/,"",name); if(name!~/^[A-Za-z0-9][A-Za-z0-9_.-]*$/ || seen[name]++) exit 1; n++}
 END {if(n==0 || n>4096) exit 1}' "$manifest" || die 'invalid or duplicate manifest entry'
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT INT TERM
printf 'werkbord/release/v1\000%s\000' "$tag" > "$work/message"
cat "$manifest" >> "$work/message"
openssl pkeyutl -verify -pubin -inkey "$key" -rawin -in "$work/message" -sigfile "$signature" >/dev/null 2>&1 || die 'signature rejected; OpenSSL with Ed25519 support is required'
