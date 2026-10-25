#!/usr/bin/env bash
# Packages the Linux agent built by build_linux.sh for Arch Linux, with the
# same layout as the .deb (build_deb.sh):
#
#   /usr/lib/usbridge-agent/usbridge-agent            agent binary
#   /usr/lib/usbridge-agent/usbridge-streamer-launch  KMS launcher
#   /usr/lib/usbridge-agent/pacman-upgrade            run by the agent through
#                                                     pkexec to update itself
#                                                     (internal/update/apply_linux.go)
#   /usr/bin/usbridge-agent                           symlink
#
# plus the USBridge pacman repository, so a package installed by hand with
# pacman -U keeps updating through pacman (see ../../scripts/arch/lib.sh).
set -euo pipefail
umask 022

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
# shellcheck source=../../scripts/arch/lib.sh
source "$REPO_ROOT/../scripts/arch/lib.sh"

VERSION="$(tr -d ' \t\n\r' < "$REPO_ROOT/VERSION")"
DIST_DIR="$REPO_ROOT/dist/linux"
LIBDIR="$(arch_libdir usbridge-agent)"

for f in usbridge-agent usbridge-streamer-launch; do
    [[ -x "$DIST_DIR/$f" ]] || { echo "missing $DIST_DIR/$f — run build_linux.sh first" >&2; exit 1; }
done

ROOT="$DIST_DIR/arch"
rm -rf "$ROOT"
mkdir -p "$ROOT$LIBDIR" "$ROOT/usr/bin" "$ROOT/usr/share/applications" "$ROOT/usr/share/icons/hicolor/256x256/apps"

install -m 0755 "$DIST_DIR/usbridge-agent" "$ROOT$LIBDIR/usbridge-agent"
install -m 0755 "$DIST_DIR/usbridge-streamer-launch" "$ROOT$LIBDIR/usbridge-streamer-launch"
ln -s "../lib/usbridge-agent/usbridge-agent" "$ROOT/usr/bin/usbridge-agent"
arch_add_repo "$ROOT" usbridge-agent "$LIBDIR"

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
arch_build "$ROOT" "$REPO_ROOT/dist" usbridge-agent "$VERSION" \
    "glibc gcc-libs libglvnd mesa libdrm vulkan-icd-loader libx11 libxcb libxrandr libxinerama libxi libxcursor libxxf86vm libxkbcommon wayland libpipewire libgpg-error" \
    "polkit: in-app updates" \
    "USBridge Agent - remote access host"
