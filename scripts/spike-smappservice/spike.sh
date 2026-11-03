#!/bin/sh
# THROWAWAY SPIKE (docs/SPIKE_SMAPPSERVICE.md). Builds, installs and drives the stand-in app and service. Nothing here needs sudo.
#   SIGN_IDENTITY="Developer ID Application: …" ./spike.sh build v1     (default: ad hoc, "-")
set -eu
HERE=$(cd "$(dirname "$0")" && pwd)
WORK=${SPIKE_WORK:-/tmp/werkbord-spike}
HOME_APP="$HOME/Applications/WerkbordSpike/Spike.app"
ID=${SIGN_IDENTITY:--}
cmd=${1:-help}; shift || true
case "$cmd" in
build) # build <version> : $WORK/<version>/Spike.app
  v=$1; out="$WORK/$v/Spike.app"; rm -rf "$WORK/$v"; mkdir -p "$out/Contents/MacOS" "$out/Contents/Library/LaunchDaemons"
  cp "$HERE/Info.plist" "$out/Contents/Info.plist"
  cp "$HERE/dev.werkbord.spike.plist" "$out/Contents/Library/LaunchDaemons/"
  clang -fobjc-arc -Wno-deprecated-declarations -framework Foundation -framework ServiceManagement "$HERE/spike.m" -o "$out/Contents/MacOS/spike"
  clang -Wno-deprecated-declarations -DSPIKE_VERSION="\"$v\"" -framework Security -framework CoreFoundation "$HERE/spiked.c" -o "$out/Contents/MacOS/spiked"
  codesign --force --sign "$ID" --identifier dev.werkbord.spike.daemon "$out/Contents/MacOS/spiked"
  codesign --force --sign "$ID" --identifier dev.werkbord.spike "$out"
  codesign --verify --strict --deep "$out" && echo "built $out ($v, signed with $ID)" ;;
install) # install <version> : put it where a person would (user-owned), replacing what is there, as dragging in a new app does
  v=$1; mkdir -p "$(dirname "$HOME_APP")"; rm -rf "$HOME_APP"; ditto "$WORK/$v/Spike.app" "$HOME_APP"; echo "installed $v at $HOME_APP" ;;
status)   "$HOME_APP/Contents/MacOS/spike" status ;;
register) "$HOME_APP/Contents/MacOS/spike" register ;;
settings) "$HOME_APP/Contents/MacOS/spike" open-settings ;;
unregister) "$HOME_APP/Contents/MacOS/spike" unregister ;;
state)    curl -s --max-time 3 http://127.0.0.1:47633/state ;;
exit)     curl -s --max-time 3 -X POST http://127.0.0.1:47633/exit ;;
cleanup)  curl -s --max-time 5 -X POST http://127.0.0.1:47633/cleanup ;;
help|*)   sed -n '2,3p' "$0" ;;
esac
