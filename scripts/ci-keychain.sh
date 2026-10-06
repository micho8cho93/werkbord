#!/bin/sh
# A temporary keychain for signing in CI (and for the tests of that), so a certificate is never put
# in anyone's login keychain, never written to disk outside a private temporary directory, and is
# gone when the job ends.
#
#   scripts/ci-keychain.sh create [--require]
#   scripts/ci-keychain.sh delete [keychain]
#
# create reads the certificate from the environment, never from a file name on the command line:
#   APPLE_CERTIFICATE_P12        the Developer ID Application certificate and its private key, as
#                                a .p12 file, base64-encoded (GitHub secret)
#   APPLE_CERTIFICATE_PASSWORD   the password the .p12 was exported with (GitHub secret)
#   APPLE_SIGNING_IDENTITY       optional: which identity to use if the .p12 holds more than one
#
# It makes a keychain with a random password, imports the identity so that only Apple's own
# tools (codesign) may use it without asking, and says where it is and which identity to sign
# with: as KEYCHAIN= and IDENTITY= lines, and as CODESIGN_KEYCHAIN / CODESIGN_IDENTITY in
# $GITHUB_ENV, which is what scripts/build-desktop.sh reads. The decoded certificate lives in a
# mode-700 directory under $RUNNER_TEMP and is removed as soon as it has been imported.
#
# The keychain is always $RUNNER_TEMP/werkbord-signing.keychain-db (TMPDIR outside CI), so the step
# that cleans up knows it even if create failed half-way. A failed create removes what it made.
#
# With a missing secret it says which one, on one line, and exits cleanly: nothing is signed.
# --require makes that a failure instead: a release must not go on unsigned.
#
# delete removes the keychain (the one named, else the one above) and puts the user's keychain
# search list back. Run it in a step that always runs. It never fails because there was nothing to delete.
#
# Test hook: CI_KEYCHAIN_ALLOW_UNTRUSTED=1 accepts an identity macOS does not trust (a self-signed
# test certificate): it can be found and imported but macOS will not let it sign.
set -eu

say() { printf 'ci-keychain: %s\n' "$*"; }
die() { printf 'ci-keychain: %s\n' "$*" >&2; exit 1; }

SECURITY=${SECURITY:-security}
BASE=${RUNNER_TEMP:-${TMPDIR:-/tmp}}
BASE=${BASE%/}
DEFAULT_KEYCHAIN="$BASE/werkbord-signing.keychain-db"

# remove_keychain <keychain>: delete it and put the search list back as it was.
remove_keychain() {
  kc=$1
  if [ -f "$kc.searchlist" ]; then
    # Put the list back first, so that nothing is left pointing at a keychain that is about to go.
    set --
    while IFS= read -r line; do [ -z "$line" ] || set -- "$@" "$line"; done < "$kc.searchlist"
    [ $# -eq 0 ] || $SECURITY list-keychains -d user -s "$@" 2>/dev/null || true
    rm -f "$kc.searchlist"
  fi
  if [ -e "$kc" ]; then
    $SECURITY delete-keychain "$kc" 2>/dev/null || rm -f "$kc"
    return 0
  fi
  return 1
}

cmd=${1:-}
[ -n "$cmd" ] || die "usage: ci-keychain.sh create [--require] | delete [keychain]"
shift

case "$cmd" in
create)
  require=""
  for a in "$@"; do
    case "$a" in --require) require=1 ;; *) die "unknown option $a" ;; esac
  done
  missing=""
  [ -n "${APPLE_CERTIFICATE_P12:-}" ] || missing="APPLE_CERTIFICATE_P12"
  [ -n "${APPLE_CERTIFICATE_PASSWORD:-}" ] || missing="${missing:+$missing, }APPLE_CERTIFICATE_PASSWORD"
  if [ -n "$missing" ]; then
    msg="the secret $missing is not set (docs/DESKTOP_RELEASE.md, step 5): nothing can be signed with a Developer ID"
    if [ -n "$require" ]; then die "$msg"; fi
    say "$msg; skipping"
    exit 0
  fi
  [ "$(uname -s)" = Darwin ] || die "a keychain exists on macOS only"

  kc=$DEFAULT_KEYCHAIN
  remove_keychain "$kc" || true # one left by an earlier run on this machine
  work=$(mktemp -d "$BASE/werkbord-signing.XXXXXX")
  ok=""
  # Whatever happens, the decoded certificate is not left behind, and a keychain that is not usable is not either.
  trap 'rm -rf "$work"; [ -n "$ok" ] || remove_keychain "$kc" >/dev/null 2>&1 || true' EXIT INT TERM
  chmod 700 "$work"
  umask 077
  printf '%s' "$APPLE_CERTIFICATE_P12" | base64 --decode > "$work/cert.p12" 2>/dev/null || die "APPLE_CERTIFICATE_P12 is not base64 (encode the .p12 with: base64 -i cert.p12)"
  [ -s "$work/cert.p12" ] || die "APPLE_CERTIFICATE_P12 is empty after decoding"

  kcpass=$(openssl rand -hex 24)
  [ -z "${GITHUB_ACTIONS:-}" ] || printf '::add-mask::%s\n' "$kcpass"
  # The search list is only changed in CI, where there is nobody's list to disturb; it is saved so that delete can restore it.
  if [ -n "${GITHUB_ACTIONS:-}" ]; then
    $SECURITY list-keychains -d user | sed 's/^[[:space:]]*"//; s/"[[:space:]]*$//' > "$kc.searchlist"
  fi

  $SECURITY create-keychain -p "$kcpass" "$kc"
  # Locked again after six hours (a release job takes minutes), not after the default five.
  $SECURITY set-keychain-settings -lut 21600 "$kc"
  $SECURITY unlock-keychain -p "$kcpass" "$kc"
  $SECURITY import "$work/cert.p12" -k "$kc" -P "$APPLE_CERTIFICATE_PASSWORD" -T /usr/bin/codesign -T /usr/bin/security >/dev/null ||
    die "the certificate could not be imported: is APPLE_CERTIFICATE_PASSWORD the password the .p12 was exported with?"
  rm -rf "$work"
  # Without this, codesign would stop to ask (on a runner, forever) whether it may use the key.
  $SECURITY set-key-partition-list -S apple-tool:,apple:,codesign: -s -k "$kcpass" "$kc" >/dev/null 2>&1 ||
    die "could not allow codesign to use the signing key"

  if [ -f "$kc.searchlist" ]; then
    # codesign is told which keychain to use (--keychain), but Apple's intermediate certificates are looked up in the list.
    set -- "$kc"
    while IFS= read -r line; do [ -z "$line" ] || set -- "$@" "$line"; done < "$kc.searchlist"
    $SECURITY list-keychains -d user -s "$@"
  fi

  if [ -n "${CI_KEYCHAIN_ALLOW_UNTRUSTED:-}" ]; then
    found=$($SECURITY find-identity -p codesigning "$kc")
  else
    found=$($SECURITY find-identity -v -p codesigning "$kc")
  fi
  # lines like:   1) 0123ABCD… "Developer ID Application: Name (TEAMID)"
  names=$(printf '%s\n' "$found" | sed -n 's/^[[:space:]]*[0-9]*) [0-9A-Fa-f]* "\(.*\)".*/\1/p')
  if [ -z "${CI_KEYCHAIN_ALLOW_UNTRUSTED:-}" ]; then
    names=$(printf '%s\n' "$names" | grep '^Developer ID Application: ' || true)
  fi
  count=$(printf '%s\n' "$names" | grep -c . || true)
  if [ -n "${APPLE_SIGNING_IDENTITY:-}" ]; then
    printf '%s\n' "$names" | grep -qxF "$APPLE_SIGNING_IDENTITY" ||
      die "APPLE_SIGNING_IDENTITY is not an identity in the certificate (it holds $count)"
    identity=$APPLE_SIGNING_IDENTITY
  else
    [ "$count" -eq 1 ] ||
      die "the certificate must hold exactly one valid \"Developer ID Application\" identity; found $count. Export that one certificate with its private key, or set APPLE_SIGNING_IDENTITY"
    identity=$names
  fi

  ok=1
  say "keychain ready; signing identity: $identity"
  printf 'KEYCHAIN=%s\nIDENTITY=%s\n' "$kc" "$identity"
  if [ -n "${GITHUB_ENV:-}" ]; then
    {
      printf 'CODESIGN_KEYCHAIN=%s\n' "$kc"
      printf 'CODESIGN_IDENTITY=%s\n' "$identity"
    } >> "$GITHUB_ENV"
  fi
  ;;

delete)
  kc=${1:-${CODESIGN_KEYCHAIN:-$DEFAULT_KEYCHAIN}}
  if remove_keychain "$kc"; then say "deleted the temporary keychain"; else say "no keychain at that path: nothing to delete"; fi
  ;;

*) die "unknown command $cmd (create or delete)" ;;
esac
