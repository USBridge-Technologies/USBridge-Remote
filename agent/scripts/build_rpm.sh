#!/usr/bin/env bash
# Packages the Linux agent built by build_linux.sh as an RPM for Fedora and
# RHEL-family systems, with the same layout as the .deb (build_deb.sh):
#
#   /usr/lib/usbridge-agent/usbridge-agent            agent binary
#   /usr/lib/usbridge-agent/usbridge-streamer-launch  KMS launcher
#   /usr/lib/usbridge-agent/dnf-upgrade               run by the agent through
#                                                     pkexec to update itself
#                                                     (internal/update/apply_linux.go)
#   /usr/bin/usbridge-agent                           symlink
#
# plus the USBridge dnf repository, so an RPM installed by hand keeps
# updating through dnf (see ../../scripts/rpm/lib.sh).
set -euo pipefail
umask 022

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
# shellcheck source=../../scripts/rpm/lib.sh
source "$REPO_ROOT/../scripts/rpm/lib.sh"

VERSION="$(tr -d ' \t\n\r' < "$REPO_ROOT/VERSION")"
DIST_DIR="$REPO_ROOT/dist/linux"
LIBDIR=/usr/lib/usbridge-agent

for f in usbridge-agent usbridge-streamer-launch; do
    [[ -x "$DIST_DIR/$f" ]] || { echo "missing $DIST_DIR/$f — run build_linux.sh first" >&2; exit 1; }
done

ROOT="$DIST_DIR/rpm"
rm -rf "$ROOT"
mkdir -p "$ROOT$LIBDIR" "$ROOT/usr/bin" "$ROOT/usr/share/applications" "$ROOT/usr/share/icons/hicolor/256x256/apps"

install -m 0755 "$DIST_DIR/usbridge-agent" "$ROOT$LIBDIR/usbridge-agent"
install -m 0755 "$DIST_DIR/usbridge-streamer-launch" "$ROOT$LIBDIR/usbridge-streamer-launch"
ln -s "../lib/usbridge-agent/usbridge-agent" "$ROOT/usr/bin/usbridge-agent"
rpm_add_repo "$ROOT" "$LIBDIR"

install -m 0644 "$REPO_ROOT/assets/icons/appicon-256.png" "$ROOT/usr/share/icons/hicolor/256x256/apps/usbridge-agent.png"
cat > "$ROOT/usr/share/applications/usbridge-agent.desktop" <<'DESKTOP'
[Desktop Entry]
Type=Application
Name=USBridge Agent
Comment=Let this computer be reached remotely with USBridge
Exec=usbridge-agent
Icon=usbridge-agent
Terminal=false
Categories=Network;RemoteAccess;
DESKTOP

# Depends: what the agent links (GL, X11 through Fyne's GLFW) and what the
# streamers it downloads (Sunshine, punktfunk-host: see internal/forkrelease)
# link or load without bundling — checked with ldd on clean installs.
rpm_build "$ROOT" "$REPO_ROOT/dist" usbridge-agent "$VERSION" \
    "libc, libstdc++.so.6()(64bit), libGL.so.1()(64bit), libEGL.so.1()(64bit), libgbm.so.1()(64bit), libdrm.so.2()(64bit), libvulkan.so.1()(64bit), libX11.so.6()(64bit), libX11-xcb.so.1()(64bit), libxcb.so.1()(64bit), libXrandr.so.2()(64bit), libXinerama.so.1()(64bit), libXi.so.6()(64bit), libXcursor.so.1()(64bit), libXxf86vm.so.1()(64bit), libxkbcommon.so.0()(64bit), libwayland-client.so.0()(64bit), libpipewire-0.3.so.0()(64bit), libgpg-error.so.0()(64bit)" \
    "polkit" \
    "USBridge Agent - remote access host" \
    "Streams this computer's desktop to USBridge clients and shares its USB
devices. Downloads the selected streamer (USBridge Streamer or Sunshine)
on first run and keeps it up to date."
