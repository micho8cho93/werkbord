#!/bin/sh
# A codesign for tests, used in place of the real one through CODESIGN= (scripts/test-desktop-sign.sh,
# scripts/test-notarize-desktop.sh).
#
# macOS will not let codesign use a self-signed identity unless it is added to the trust settings, which a test
# must not change, so a test cannot sign with a Developer ID. This does what it can for real: it SIGNS
# for real (ad hoc, with the hardened runtime and the entitlements it was asked for), records what it was asked
# (SIGN lines in $SHIM_DIR/log), and when asked to DESCRIBE a signature (-dvv) adds what a certificate would have
# added: the authority, the team, the timestamp, and no "adhoc". A signature is known by the hash of the code it
# seals, so a copy of a signed app is still the signed app. It needs $SHIM_DIR (a directory the test owns).
#
# SHIM_BREAK="<what>:<where>" makes it misbehave on the paths that contain <where> ("@" first: that end with it):
#   skip (sign nothing), noruntime, adhoc, notimestamp, team (another team's identity).
real=/usr/bin/codesign
rec="$SHIM_DIR/records"
# A signature is known by the hash of the code it seals, so a copy of a signed app is still the signed app.
cdhash() { $real -dvvv "$1" 2>&1 | sed -n 's/^CDHash=//p' | head -1; }
case " $* " in
*" -dvv "*)
  target=$(eval "printf '%s' \"\${$#}\"")
  out=$($real "$@" 2>&1) || { printf '%s\n' "$out"; exit 1; }
  r="$rec/$(cdhash "$target")"
  if [ -f "$r" ]; then
    . "$r"
    team=$(printf '%s' "$identity" | sed -n 's/.*(\(.*\)).*/\1/p')
    out=$(printf '%s\n' "$out" | grep -v '^Signature=adhoc' | grep -v '^TeamIdentifier=' | grep -v '^Authority=' | sed 's/flags=0x[0-9a-f]*(adhoc,runtime)/flags=0x10000(runtime)/; s/flags=0x2(adhoc)/flags=0x0(none)/')
    out="$out
Authority=$identity
Authority=Developer ID Certification Authority
Authority=Apple Root CA
TeamIdentifier=$team"
    [ "$timestamp" = none ] || out="$out
Timestamp=Oct 6, 2026 at 12:00:00"
    case $runtime in 1) ;; *) out=$(printf '%s\n' "$out" | sed 's/flags=0x10000(runtime)/flags=0x0(none)/') ;; esac
  fi
  printf '%s\n' "$out"
  exit 0 ;;
*" --force "*)
  identity=""; keychain=""; runtime=0; timestamp=secure; ent=""; target=""
  while [ $# -gt 0 ]; do
    case $1 in
      --sign) identity=$2; shift ;;
      --keychain) keychain=$2; shift ;;
      --options) [ "$2" = runtime ] && runtime=1; shift ;;
      --timestamp=none) timestamp=none ;;
      --timestamp) timestamp=secure ;;
      --entitlements) ent=$2; shift ;;
      --force) ;;
      *) target=$1 ;;
    esac
    shift
  done
  printf 'SIGN %s | identity=%s | runtime=%s | timestamp=%s | keychain=%s | entitlements=%s\n' "$target" "$identity" "$runtime" "$timestamp" "$keychain" "$ent" >> "$SHIM_DIR/log"
  # What the test asks to go wrong: "<what>:<where>", where being a part of the path ("@" in front: the end of it).
  what=${SHIM_BREAK%%:*}; where=${SHIM_BREAK#*:}
  hit=""
  if [ -n "${SHIM_BREAK:-}" ]; then
    case $where in
      @*) case $target in *"${where#@}") hit=1 ;; esac ;;
      *) case $target in *"$where"*) hit=1 ;; esac ;;
    esac
  fi
  if [ -n "$hit" ]; then
    case $what in
      skip) exit 0 ;;
      noruntime) runtime=0 ;;
      adhoc) identity=- ;;
      notimestamp) timestamp=none ;;
      team) identity="Developer ID Application: Someone Else (OTHER00000)" ;;
    esac
  fi
  set -- --force --sign -
  [ "$runtime" = 0 ] || set -- "$@" --options runtime
  [ -z "$ent" ] || set -- "$@" --entitlements "$ent"
  $real "$@" "$target" || exit 1
  if [ "$identity" != - ]; then
    printf 'identity="%s"\nruntime=%s\ntimestamp=%s\n' "$identity" "$runtime" "$timestamp" > "$rec/$(cdhash "$target")"
  fi
  exit 0 ;;
*) exec $real "$@" ;;
esac
