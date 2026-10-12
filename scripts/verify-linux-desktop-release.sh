#!/bin/sh
# Download the public Linux assets for one architecture and check their sidecar hashes and package contents.
set -eu

die() { printf 'verify-linux-desktop-release: FAILED: %s\n' "$*" >&2; exit 1; }
[ "$#" -eq 2 ] || die "usage: scripts/verify-linux-desktop-release.sh werkbord-vX.Y.Z amd64|arm64"
tag=$1
arch=$2
case "$tag" in werkbord-v[0-9]*.[0-9]*.[0-9]*) ;; *) die "invalid release tag: $tag" ;; esac
case "$arch" in amd64|arm64) ;; *) die "unsupported Linux architecture: $arch" ;; esac
version=${tag#werkbord-v}
base=${WERKBORD_RELEASE_BASE:-https://github.com/micho8cho93/werkbord/releases/download}
for tool in curl sha256sum dpkg-deb tar; do command -v "$tool" >/dev/null 2>&1 || die "$tool is required"; done

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT INT TERM
case "$base" in file://*) retry= ;; *) retry='--retry 6 --retry-delay 10 --retry-all-errors' ;; esac
stem="Werkbord_${version}_linux_${arch}"
for file in "$stem.deb" "$stem.deb.sha256" "$stem.tar.gz" "$stem.tar.gz.sha256"; do
  # shellcheck disable=SC2086
  curl -fsSL $retry -o "$work/$file" "$base/$tag/$file" || die "the public release does not serve $file"
done
(cd "$work" && cat "$stem.deb.sha256" "$stem.tar.gz.sha256" | sha256sum -c -) || die "a Linux package hash does not match its published sidecar"
[ "$(dpkg-deb -f "$work/$stem.deb" Architecture)" = "$arch" ] || die "the Debian package architecture is wrong"
tar -tzf "$work/$stem.tar.gz" | grep -F "$stem/app/bin/Werkbord" >/dev/null || die "the portable archive has no desktop executable"
tar -tzf "$work/$stem.tar.gz" | grep -F "$stem/app/Helpers/werkbord-team" >/dev/null || die "the portable archive has no Team service"
printf 'verify-linux-desktop-release: %s public packages are reachable, checksummed and contain the unified app\n' "$arch"
