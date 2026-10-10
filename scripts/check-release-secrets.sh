#!/bin/sh
# Says, before anything is built, whether the secrets a desktop release needs are there and look like
# what they should, without ever printing one.
#
#   scripts/check-release-secrets.sh desktop   the signing, notarization, update-signing and license issuer secrets
#
# They come from the environment, which is where the workflow puts the repository's secrets. A missing one is named,
# on one line, with the product's release setup guide, and the exit is non-zero: a release must not
# go on without them. A present one is checked for the mistake it is most likely to hold (the wrong file pasted,
# a .p12 that was not base64-encoded), so that a dry run finds that out in a minute and not after the build.
#
# Secret                       step   what it is
#   APPLE_CERTIFICATE_P12          5     the Developer ID Application certificate and key (made in steps 2-3), .p12, base64
#   APPLE_CERTIFICATE_PASSWORD     5     the password it was exported with
#   APPLE_NOTARY_KEY               5     the App Store Connect API key file (made in step 4: AuthKey_XXXX.p8), as text
#   NOTARY_KEY_ID                  5     its Key ID (the workflow's name for the APPLE_NOTARY_KEY_ID secret)
#   NOTARY_ISSUER                  5     the Issuer ID, a UUID (the APPLE_NOTARY_ISSUER secret)
#   SPARKLE_ED_PRIVATE_KEY         6     the private half of the update-signing key
#   LICENSE_ISSUER_PUBLIC_KEY            Team's 32-byte Ed25519 public key (TEAM_LICENSE_ISSUER_PUBLIC_KEY secret); the app carries
#                                        Team's installer, which checks every license with it
# APPLE_SIGNING_IDENTITY is optional (only when the certificate holds several identities).
set -eu

kind=${1:-}
case "$kind" in
  desktop) guide=docs/DESKTOP_RELEASE.md ;;
  *) echo "usage: check-release-secrets.sh desktop" >&2; exit 2 ;;
esac

missing=""
wrong=""
need() { # <env name> <secret name shown> <step>
  eval "value=\${$1:-}"
  if [ -z "$value" ]; then
    label=$2
    case "$3" in license) ;; *) label="$label (step $3)" ;; esac
    missing="${missing:+$missing; }$label"
    return 1
  fi
  return 0
}
bad() { wrong="${wrong:+$wrong; }$1"; }

if need APPLE_CERTIFICATE_P12 APPLE_CERTIFICATE_P12 5; then
  printf '%s' "$APPLE_CERTIFICATE_P12" | base64 --decode > /dev/null 2>&1 || bad "APPLE_CERTIFICATE_P12 is not base64 (encode the .p12 with: base64 -i cert.p12)"
fi
need APPLE_CERTIFICATE_PASSWORD APPLE_CERTIFICATE_PASSWORD 5 || true
if need APPLE_NOTARY_KEY APPLE_NOTARY_KEY 5; then
  case "$APPLE_NOTARY_KEY" in
    *"BEGIN PRIVATE KEY"*) ;;
    *) bad "APPLE_NOTARY_KEY is not the text of the .p8 key file (it should start with -----BEGIN PRIVATE KEY-----)" ;;
  esac
fi
if need NOTARY_KEY_ID APPLE_NOTARY_KEY_ID 5; then
  printf '%s' "$NOTARY_KEY_ID" | grep -Eq '^[A-Z0-9]{10}$' || bad "APPLE_NOTARY_KEY_ID should be the 10 letters and digits of the key's Key ID"
fi
if need NOTARY_ISSUER APPLE_NOTARY_ISSUER 5; then
  printf '%s' "$NOTARY_ISSUER" | grep -Eq '^[0-9a-fA-F]{8}-([0-9a-fA-F]{4}-){3}[0-9a-fA-F]{12}$' || bad "APPLE_NOTARY_ISSUER should be the Issuer ID, a UUID"
fi
if [ ! -s "${SPARKLE_PUBLIC_KEY_FILE:-$(dirname "$0")/../desktop/build/darwin/sparkle-public-key}" ]; then
  missing="${missing:+$missing; }desktop/build/darwin/sparkle-public-key, the update-signing public key that is committed (step 6)"
fi
if need SPARKLE_ED_PRIVATE_KEY SPARKLE_ED_PRIVATE_KEY 6; then
  printf '%s' "$SPARKLE_ED_PRIVATE_KEY" | grep -Eq '^[A-Za-z0-9+/=]{40,}$' || bad "SPARKLE_ED_PRIVATE_KEY should be one line of base64 (what generate_keys -x wrote)"
fi
if need LICENSE_ISSUER_PUBLIC_KEY TEAM_LICENSE_ISSUER_PUBLIC_KEY license; then
  if [ ${#LICENSE_ISSUER_PUBLIC_KEY} -ne 43 ]; then
    bad "TEAM_LICENSE_ISSUER_PUBLIC_KEY must be a 32-byte Ed25519 public key encoded as raw URL base64 (43 characters, no padding)"
  else
    case "$LICENSE_ISSUER_PUBLIC_KEY" in
      *[!A-Za-z0-9_-]*) bad "TEAM_LICENSE_ISSUER_PUBLIC_KEY must use raw URL base64 (letters, digits, - and _)" ;;
    esac
  fi
fi

if [ -n "$missing" ] || [ -n "$wrong" ]; then
  [ -z "$missing" ] || echo "::error::release: missing secrets: $missing. Nothing was built. See $guide." >&2
  [ -z "$wrong" ] || echo "::error::release: secrets that look wrong: $wrong. See $guide." >&2
  exit 1
fi
echo "release: all the $kind release secrets are present and look right"
