#!/bin/sh
set -eu
[ "$#" -eq 1 ] || exit 1
printf '%s' "$1" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-preview\.[1-9][0-9]*)?$' || { echo 'invalid unified version' >&2; exit 1; }
printf '%s\n' "$1" | sed 's/-preview\./~preview./'
