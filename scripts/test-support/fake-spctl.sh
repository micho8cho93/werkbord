#!/bin/sh
# Gatekeeper's spctl, for the tests: accepts as a Notarized Developer ID unless $FAKE_MODE is gatekeeper-rejects.
echo "spctl $*" >> "$FAKE_LOG"
if [ "$FAKE_MODE" = gatekeeper-rejects ]; then echo "$* : rejected" >&2; exit 3; fi
last=$(eval "printf '%s' \"\${$#}\"")
echo "$last: accepted"; echo "source=Notarized Developer ID"
