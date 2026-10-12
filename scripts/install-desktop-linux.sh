#!/bin/sh
# Install the Linux desktop bundle in a root-owned path so the app can safely
# request the administrator's approval when it installs Team's system service.
set -eu

die() { printf 'werkbord desktop installer: %s\n' "$*" >&2; exit 1; }
[ "$(id -u)" -eq 0 ] || die "run with sudo so the installed app is protected from unprivileged changes"

HERE=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
APP="$HERE/app"
[ -x "$APP/bin/Werkbord" ] || die "the desktop app is missing from this archive"
[ -x "$APP/Helpers/werkbord-team" ] || die "Team's service is missing from this archive"

PREFIX=/opt/werkbord
mkdir -p "$PREFIX" /usr/share/applications /usr/share/icons/hicolor/1024x1024/apps
cp -R "$APP/." "$PREFIX/"
chown -R root:root "$PREFIX"
chmod -R u+rwX,go-w "$PREFIX"
install -m 0644 "$HERE/share/werkbord.desktop" /usr/share/applications/werkbord.desktop
install -m 0644 "$HERE/share/werkbord.png" /usr/share/icons/hicolor/1024x1024/apps/werkbord.png
printf 'Installed Werkbord in %s. The Team service and its data are managed separately and remain installed if this app is removed.\n' "$PREFIX"
