#!/bin/sh
# Says whether a Werkbord.app (or disk image) is signed the way it is meant to be. One definition
# of that, used by the build, the tests and the check of a published release.
#
#   scripts/check-desktop-signature.sh --adhoc          <Werkbord.app>
#   scripts/check-desktop-signature.sh --distribution   <Werkbord.app | Werkbord_….dmg>
#   scripts/check-desktop-signature.sh --distribution --no-timestamp <…>     (tests only)
#
# --adhoc: this is a build for the computer that made it. The signature must be valid, and every piece
#   of code in the bundle must be signed the same way (a half-signed bundle is the usual way a
#   signature breaks on someone else's Mac).
#
# --distribution: this is what Apple will notarize and Gatekeeper will open. The signature must be valid
#   and strict; EVERY Mach-O file in the bundle (the window, the program it carries, anything added later,
#   such as an updater and its helpers) must be signed with a Developer ID Application certificate, never
#   ad hoc, by the same team, with the hardened runtime and a secure timestamp; and the app may hold only
#   the entitlements desktop/build/darwin/entitlements.plist lists (none today), so that a hardened
#   runtime exception can only arrive through a reviewed change to that file.
#
# CODESIGN names the codesign program (tests substitute a recorder).
set -eu

die() { printf 'check-desktop-signature: %s\n' "$*" >&2; exit 1; }

mode=""
timestamp=1
target=""
for a in "$@"; do
  case "$a" in
    --adhoc) mode=adhoc ;;
    --distribution) mode=distribution ;;
    --no-timestamp) timestamp="" ;;
    -*) die "unknown option $a" ;;
    *) target=$a ;;
  esac
done
[ -n "$mode" ] && [ -n "$target" ] || die "usage: check-desktop-signature.sh --adhoc|--distribution [--no-timestamp] <Werkbord.app|disk image>"
[ -e "$target" ] || die "$target does not exist"
CODESIGN=${CODESIGN:-codesign}
root=$(cd "$(dirname "$0")/.." && pwd)
ENTITLEMENTS=${ENTITLEMENTS:-$root/desktop/build/darwin/entitlements.plist}

"$CODESIGN" --verify --strict "$target" 2>/dev/null || {
  "$CODESIGN" --verify --strict --verbose=2 "$target" >&2 || true
  die "$target does not verify"
}

# A disk image holds no code of its own: it is signed (so that Gatekeeper says who made it), nothing more.
case "$target" in
*.dmg)
  [ "$mode" = distribution ] || die "a disk image is checked with --distribution"
  info=$("$CODESIGN" -dvv "$target" 2>&1) || die "$target is not signed"
  case $info in *Signature=adhoc*) die "$target is signed ad hoc" ;; esac
  case $info in *"Authority=Developer ID Application"*) ;; *) die "$target is not signed by a Developer ID Application certificate" ;; esac
  [ -z "$timestamp" ] || case $info in *Timestamp=*) ;; *) die "$target has no secure timestamp" ;; esac
  exit 0
  ;;
esac

"$CODESIGN" --verify --strict --deep "$target" 2>/dev/null || die "$target does not verify (--deep)"

# Every Mach-O file, one per line. Anything that is code and not in this list would be unsigned.
list=$(mktemp)
trap 'rm -f "$list"' EXIT INT TERM
find "$target" -type f ! -name '*.png' ! -name '*.icns' ! -name '*.plist' ! -name '*.strings' ! -name '*.nib' ! -name '*.html' ! -name '*.js' ! -name '*.css' ! -name '*.svg' -print |
  while IFS= read -r f; do
    case $(file -b "$f") in *Mach-O*) printf '%s\n' "$f" ;; esac
  done > "$list"
[ -s "$list" ] || die "found no code in $target"

team=""
while IFS= read -r f; do
  info=$("$CODESIGN" -dvv "$f" 2>&1) || die "$f is not signed"
  if [ "$mode" = adhoc ]; then
    case $info in *Signature=adhoc*) ;; *) die "$f is not signed ad hoc like the rest of the bundle" ;; esac
    continue
  fi
  case $info in *Signature=adhoc*) die "$f is signed ad hoc: a build for other computers is signed with the Developer ID" ;; esac
  case $info in *"Authority=Developer ID Application"*) ;; *) die "$f is not signed by a Developer ID Application certificate" ;; esac
  case $info in *"flags=0x"*"(runtime)"*) ;; *) die "$f is signed without the hardened runtime, which notarization requires" ;; esac
  [ -z "$timestamp" ] || case $info in *Timestamp=*) ;; *) die "$f has no secure timestamp, which notarization requires" ;; esac
  t=$(printf '%s\n' "$info" | sed -n 's/^TeamIdentifier=//p')
  case $t in ""|"not set") die "$f has no Team ID" ;; esac
  if [ -z "$team" ]; then team=$t; elif [ "$team" != "$t" ]; then die "$f is signed by team $t, the rest by $team"; fi
done < "$list"

if [ "$mode" = distribution ]; then
  # The entitlements the app carries are exactly the ones the repository's file says.
  have=$("$CODESIGN" -d --entitlements - --xml "$target" 2>/dev/null | tr -d '[:space:]' | sed -n 's/.*<dict>\(.*\)<\/dict>.*/\1/p' || true)
  want=$(tr -d '[:space:]' < "$ENTITLEMENTS" | sed 's/<!--.*-->//; s/.*<dict>\(.*\)<\/dict>.*/\1/')
  [ "$have" = "$want" ] || die "the app's entitlements differ from $ENTITLEMENTS: has \"$have\", expected \"$want\""
fi
printf 'check-desktop-signature: %s is signed for %s (%s)\n' "$target" "$mode" "$(wc -l < "$list" | tr -d ' ') pieces of code"
