#!/bin/sh
# Public offline review input. Never creates or reads a private key.
set -eu
app=${1:?Team app required}
version=${2:?Team version required}
case "$version" in v[0-9]*.[0-9]*.[0-9]*) ;; *) exit 1 ;; esac
printf '{"tag":"werkbord-team-%s","files":{' "$version"
sep=""
for component in Helpers/werkbord-team Helpers/nebula Helpers/rqlited Helpers/werkbord Resources/rqlited.build; do
  hash=$(shasum -a 256 "$app/Contents/$component" | cut -d' ' -f1)
  printf '%s"%s":"%s"' "$sep" "$component" "$hash"
  sep=,
done
printf '}}\n'
