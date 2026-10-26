#!/usr/bin/env bash
# Packages the client AppDir that build_linux.sh assembled for Arch Linux,
# with the same layout as the .deb (build_deb.sh):
#
#   /opt/usbridge-client/usr/...           the AppDir's usr/ tree, unchanged
#   /opt/usbridge-client/pacman-upgrade    run through pkexec by the update
#                                          dialog (internal/update/apply_linux.go)
#   /usr/bin/usbridge-client               symlink
#
# plus the USBridge pacman repository (../../scripts/arch/lib.sh).
set -euo pipefail
umask 022

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
# shellcheck source=../../scripts/arch/lib.sh
source "$REPO_ROOT/../scripts/arch/lib.sh"

VERSION="$(tr -d ' \t\n\r' < "$REPO_ROOT/VERSION")"
APPDIR="$REPO_ROOT/dist/linux/AppDir"
LIBDIR="$(arch_libdir usbridge-client)"

[[ -x "$APPDIR/usr/bin/usbridge-client" ]] || { echo "missing $APPDIR/usr/bin/usbridge-client — run build_linux.sh first" >&2; exit 1; }

ROOT="$REPO_ROOT/dist/linux/arch"
rm -rf "$ROOT"
mkdir -p "$ROOT$LIBDIR" "$ROOT/usr/bin" "$ROOT/usr/share/applications" "$ROOT/usr/share/icons/hicolor/256x256/apps"

cp -a "$APPDIR/usr" "$ROOT$LIBDIR/"
rm -rf "$ROOT$LIBDIR/usr/share/applications" "$ROOT$LIBDIR/usr/share/icons"
chmod -R u+rwX,go+rX,go-w "$ROOT$LIBDIR"
ln -s "$LIBDIR/usr/bin/usbridge-client" "$ROOT/usr/bin/usbridge-client"
arch_add_repo "$ROOT" usbridge-client "$LIBDIR"

install -m 0644 "$REPO_ROOT/Icon.png" "$ROOT/usr/share/icons/hicolor/256x256/apps/usbridge-client.png"
cat > "$ROOT/usr/share/applications/usbridge-client.desktop" <<'DESKTOP'
[Desktop Entry]
Type=Application
Name=USBridge Client
Comment=Connect to your computers with USBridge
Exec=usbridge-client %u
Icon=usbridge-client
Terminal=false
Categories=Network;RemoteAccess;
MimeType=x-scheme-handler/usbridge;
DESKTOP

arch_build "$ROOT" "$REPO_ROOT/dist" usbridge-client "$VERSION" \
    "glibc libglvnd libx11 fontconfig freetype2 harfbuzz fribidi libva libvdpau vulkan-icd-loader libdrm" \
    "usbridge-agent: let this computer be reached too
polkit: in-app updates" \
    "USBridge Client - remote desktop and USB passthrough"
