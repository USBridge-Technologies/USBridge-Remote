#!/usr/bin/env bash
# Packages the client AppDir that build_linux.sh assembled as an RPM for
# Fedora and RHEL-family systems, with the same layout as the .deb
# (build_deb.sh):
#
#   /opt/usbridge-client/usr/...         the AppDir's usr/ tree, unchanged
#   /opt/usbridge-client/dnf-upgrade     run through pkexec by the update
#                                        dialog (internal/update/apply_linux.go)
#   /usr/bin/usbridge-client             symlink
#
# plus the USBridge dnf repository (../../scripts/rpm/lib.sh). Recommends
# usbridge-agent, so dnf brings the agent along unless weak dependencies
# are off.
set -euo pipefail
umask 022

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
# shellcheck source=../../scripts/rpm/lib.sh
source "$REPO_ROOT/../scripts/rpm/lib.sh"

VERSION="$(tr -d ' \t\n\r' < "$REPO_ROOT/VERSION")"
APPDIR="$REPO_ROOT/dist/linux/AppDir"
LIBDIR=/opt/usbridge-client

[[ -x "$APPDIR/usr/bin/usbridge-client" ]] || { echo "missing $APPDIR/usr/bin/usbridge-client — run build_linux.sh first" >&2; exit 1; }

ROOT="$REPO_ROOT/dist/linux/rpm"
rm -rf "$ROOT"
mkdir -p "$ROOT$LIBDIR" "$ROOT/usr/bin" "$ROOT/usr/share/applications" "$ROOT/usr/share/icons/hicolor/256x256/apps"

cp -a "$APPDIR/usr" "$ROOT$LIBDIR/"
rm -rf "$ROOT$LIBDIR/usr/share/applications" "$ROOT$LIBDIR/usr/share/icons"
chmod -R u+rwX,go+rX,go-w "$ROOT$LIBDIR"
ln -s "$LIBDIR/usr/bin/usbridge-client" "$ROOT/usr/bin/usbridge-client"
rpm_add_repo "$ROOT" "$LIBDIR"

install -m 0644 "$REPO_ROOT/Icon.png" "$ROOT/usr/share/icons/hicolor/256x256/apps/usbridge-client.png"
cat > "$ROOT/usr/share/applications/usbridge-client.desktop" <<'DESKTOP'
[Desktop Entry]
Type=Application
Name=USBridge Client
Comment=Connect to your computers with USBridge
Exec=usbridge-client
Icon=usbridge-client
Terminal=false
Categories=Network;RemoteAccess;
DESKTOP

rpm_build "$ROOT" "$REPO_ROOT/dist" usbridge-client "$VERSION" \
    "libc, libGL.so.1()(64bit), libX11.so.6()(64bit), libfontconfig.so.1()(64bit), libfreetype.so.6()(64bit), libharfbuzz.so.0()(64bit), libfribidi.so.0()(64bit), libva.so.2()(64bit), libva-drm.so.2()(64bit), libva-x11.so.2()(64bit), libvdpau.so.1()(64bit), libvulkan.so.1()(64bit), libdrm.so.2()(64bit)" \
    "usbridge-agent, polkit" \
    "USBridge Client - remote desktop and USB passthrough" \
    "Connects to computers running USBridge Agent: low-latency desktop
streaming, and passing this computer's USB devices through to them."
