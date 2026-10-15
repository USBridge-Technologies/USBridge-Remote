#!/usr/bin/env bash
# Packages the Linux agent built by build_linux.sh as a .deb:
#
#   /usr/lib/usbridge-agent/usbridge-agent            agent binary
#   /usr/lib/usbridge-agent/usbridge-streamer-launch  KMS launcher, found next
#                                                     to the agent binary by
#                                                     permissions.bundledStreamerLauncher
#   /usr/lib/usbridge-agent/apt-upgrade               run by the agent through
#                                                     pkexec to update itself
#                                                     (internal/update/apply_linux.go)
#   /usr/bin/usbridge-agent                           symlink
#
# plus the USBridge APT repository, so a .deb downloaded by hand keeps
# updating through apt (see ../../scripts/deb/lib.sh).
set -euo pipefail
umask 022

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
# shellcheck source=../../scripts/deb/lib.sh
source "$REPO_ROOT/../scripts/deb/lib.sh"

VERSION="$(tr -d ' \t\n\r' < "$REPO_ROOT/VERSION")"
DIST_DIR="$REPO_ROOT/dist/linux"
OUT="$REPO_ROOT/dist/USBridgeAgent-Linux-x86_64-${VERSION}${DEB_SUFFIX:-}.deb"
LIBDIR=/usr/lib/usbridge-agent

for f in usbridge-agent usbridge-streamer-launch; do
    [[ -x "$DIST_DIR/$f" ]] || { echo "missing $DIST_DIR/$f — run build_linux.sh first" >&2; exit 1; }
done

ROOT="$DIST_DIR/deb"
rm -rf "$ROOT"
mkdir -p "$ROOT$LIBDIR" "$ROOT/usr/bin" "$ROOT/usr/share/applications" "$ROOT/usr/share/icons/hicolor/256x256/apps"

install -m 0755 "$DIST_DIR/usbridge-agent" "$ROOT$LIBDIR/usbridge-agent"
install -m 0755 "$DIST_DIR/usbridge-streamer-launch" "$ROOT$LIBDIR/usbridge-streamer-launch"
ln -s "../lib/usbridge-agent/usbridge-agent" "$ROOT/usr/bin/usbridge-agent"
deb_add_repo "$ROOT" usbridge-agent "$LIBDIR"

install -m 0644 "$REPO_ROOT/assets/icons/appicon-256.png" "$ROOT/usr/share/icons/hicolor/256x256/apps/usbridge-agent.png"
cat > "$ROOT/usr/share/applications/usbridge-agent.desktop" <<'EOF'
[Desktop Entry]
Type=Application
Name=USBridge Agent
Comment=Let this computer be reached remotely with USBridge
Exec=usbridge-agent
Icon=usbridge-agent
Terminal=false
Categories=Network;RemoteAccess;
EOF

deb_build "$ROOT" "$OUT" usbridge-agent "$VERSION" \
    "libc6, libgl1, libx11-6" "pkexec | policykit-1" net \
    "USBridge Agent - remote access host" \
    "Streams this computer's desktop to USBridge clients and shares its USB
devices. Downloads the selected streamer (USBridge Streamer or Sunshine)
on first run and keeps it up to date."
