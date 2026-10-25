#!/bin/sh
# Build the unified Linux desktop app and Team service, then produce a root-installable
# .deb and a portable archive with an installer. Run on native Linux for the target arch.
set -eu

die() { printf 'build-desktop-linux: %s\n' "$*" >&2; exit 1; }
[ "$#" -eq 3 ] || die "usage: scripts/build-desktop-linux.sh <amd64|arm64> <version> <output-dir>"
ARCH=$1
VERSION=$2
OUT=$3
case "$ARCH" in amd64|arm64) ;; *) die "unsupported Linux architecture: $ARCH" ;; esac
DEB_VERSION=$("$(dirname "$0")/debian-version.sh" "$VERSION") || die "invalid build version"
[ "$(uname -s)" = Linux ] || die "build on native Linux"
[ "$(go env GOARCH)" = "$ARCH" ] || die "use a native $ARCH runner"

cd "$(dirname "$0")/.."
ROOT=$(pwd)
RELEASE_FLAGS=$(scripts/release-build-flags.sh)
command -v dpkg-deb >/dev/null 2>&1 || die "dpkg-deb is required"
command -v tar >/dev/null 2>&1 || die "tar is required"
command -v sha256sum >/dev/null 2>&1 || die "sha256sum is required"
command -v pkg-config >/dev/null 2>&1 || die "pkg-config is required"
pkg-config --exists gtk+-3.0 webkit2gtk-4.1 || die "install GTK3 and WebKit2GTK 4.1 development files"

make web web-embed web-shell

OUT=$(mkdir -p "$OUT" && cd "$OUT" && pwd)
STAGE=$(mktemp -d)
WAILS_CONFIG="$ROOT/desktop/wails.json"
cp "$WAILS_CONFIG" "$STAGE/wails.json.original"
restore() { cp "$STAGE/wails.json.original" "$WAILS_CONFIG"; rm -rf "$STAGE"; }
trap restore EXIT HUP INT TERM

python3 - "$WAILS_CONFIG" "$VERSION" <<'PY'
import json, pathlib, sys
p = pathlib.Path(sys.argv[1])
d = json.loads(p.read_text())
d["info"]["productVersion"] = sys.argv[2]
p.write_text(json.dumps(d, indent=2) + "\n")
PY

APP="$STAGE/app"
mkdir -p "$APP/bin" "$APP/Helpers" "$APP/Resources/licenses"

echo "build-desktop-linux: building Werkbord $VERSION for linux/$ARCH"
(
  cd desktop
  CGO_ENABLED=1 go run github.com/wailsapp/wails/v2/cmd/wails@v2.17.0 build \
    -platform "linux/$ARCH" -skipfrontend -skipbindings -nosyncgo -m \
    -tags desktop,production,webkit2_41 -o Werkbord \
    -ldflags "-s -w $RELEASE_FLAGS -X main.version=v$VERSION -X devboard/desktop/internal/teaminstall.releaseBuild=v$VERSION"
)
cp desktop/build/bin/Werkbord "$APP/bin/Werkbord"

CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -trimpath -ldflags "-s -w $RELEASE_FLAGS -X main.version=v$VERSION" -o "$APP/Helpers/werkbord" ./cmd/werkbord
NEBULA=$(scripts/fetch-nebula.sh linux "$ARCH")
RQLITE=$(scripts/fetch-rqlite.sh linux "$ARCH")
cp "$NEBULA/nebula" "$APP/Helpers/nebula"
cp "$RQLITE/rqlited" "$APP/Helpers/rqlited"

DATABASE_SHA=$(sha256sum "$APP/Helpers/rqlited" | cut -d' ' -f1)
RQLITE_VERSION=$(sed -n 's/^const Version = "\(.*\)"$/\1/p' internal/team/infra/rqlite/manifest.go)
RQLITE_COMMIT=$(sed -n 's/^const SourceCommit = "\(.*\)"$/\1/p' internal/team/infra/rqlite/manifest.go)
printf '{"version":"v%s","commit":"%s","sha256":"%s"}\n' "$RQLITE_VERSION" "$RQLITE_COMMIT" "$DATABASE_SHA" > "$APP/Resources/rqlited.build"
CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -trimpath \
  -ldflags "-s -w $RELEASE_FLAGS -X main.version=v$VERSION -X devboard/internal/team/infra/rqlite.distributionBinarySHA256=$DATABASE_SHA -X devboard/internal/team/infra/nebula.distributionBinarySHA256=$(sha256sum "$APP/Helpers/nebula" | cut -d' ' -f1)" \
  -o "$APP/Helpers/werkbord-team" ./cmd/werkbord-team
cp -R third_party/nebula third_party/rqlite third_party/go "$APP/Resources/licenses/"

for path in "$APP/bin/Werkbord" "$APP/Helpers/werkbord" "$APP/Helpers/werkbord-team" "$APP/Helpers/nebula" "$APP/Helpers/rqlited"; do
  chmod 0755 "$path"
done
[ "$("$APP/Helpers/werkbord" version)" = "v$VERSION" ] || die "the controller does not report v$VERSION"
[ "$("$APP/Helpers/werkbord-team" version)" = "v$VERSION" ] || die "Team's service does not report v$VERSION"

python3 scripts/audit-release.py --out "$STAGE/inventory" || die "dependency inventory failed"
mkdir -p "$APP/Resources/licenses/dependencies"
cp "$STAGE/inventory/notices/THIRD_PARTY_LICENSES.txt" "$STAGE/inventory/notices/INDEX.json" "$STAGE/inventory/source-sbom.cdx.json" "$APP/Resources/licenses/dependencies/"

SHARE="$STAGE/share"
mkdir -p "$SHARE"
cat > "$SHARE/werkbord.desktop" <<'EOF'
[Desktop Entry]
Type=Application
Name=Werkbord
Comment=The unified Individual and Team workspace app
Exec=/opt/werkbord/bin/Werkbord
Icon=werkbord
Terminal=false
Categories=Development;Utility;
StartupWMClass=werkbord
EOF
cp desktop/build/appicon.png "$SHARE/werkbord.png"

TAR_ROOT="$STAGE/Werkbord_${VERSION}_linux_${ARCH}"
mkdir -p "$TAR_ROOT/app" "$TAR_ROOT/share"
cp -R "$APP/." "$TAR_ROOT/app/"
cp "$SHARE/werkbord.desktop" "$SHARE/werkbord.png" "$TAR_ROOT/share/"
cp scripts/install-desktop-linux.sh "$TAR_ROOT/install.sh"
chmod 0755 "$TAR_ROOT/install.sh"
tar -C "$STAGE" -czf "$OUT/Werkbord_${VERSION}_linux_${ARCH}.tar.gz" "$(basename "$TAR_ROOT")"

PKG="$STAGE/deb"
mkdir -p "$PKG/DEBIAN" "$PKG/opt/werkbord" "$PKG/usr/share/applications" "$PKG/usr/share/icons/hicolor/1024x1024/apps"
cp -R "$APP/." "$PKG/opt/werkbord/"
cp "$SHARE/werkbord.desktop" "$PKG/usr/share/applications/werkbord.desktop"
cp "$SHARE/werkbord.png" "$PKG/usr/share/icons/hicolor/1024x1024/apps/werkbord.png"
cat > "$PKG/DEBIAN/control" <<EOF
Package: werkbord
Version: $DEB_VERSION
Section: devel
Priority: optional
Architecture: $ARCH
Depends: libgtk-3-0t64 | libgtk-3-0, libwebkit2gtk-4.1-0, pkexec, systemd
Maintainer: Werkbord
Description: Unified Individual and Team workspace app
 Werkbord runs coding work locally and provides the Team workspace app.
EOF
dpkg-deb --build --root-owner-group "$PKG" "$OUT/Werkbord_${VERSION}_linux_${ARCH}.deb" >/dev/null
(cd "$OUT" && sha256sum "Werkbord_${VERSION}_linux_${ARCH}.deb" > "Werkbord_${VERSION}_linux_${ARCH}.deb.sha256" && sha256sum "Werkbord_${VERSION}_linux_${ARCH}.tar.gz" > "Werkbord_${VERSION}_linux_${ARCH}.tar.gz.sha256")

echo "build-desktop-linux: built the .deb and tar.gz for linux/$ARCH in $OUT"
