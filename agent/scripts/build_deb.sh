#!/usr/bin/env bash
# Packages the Linux agent built by build_linux.sh as a .deb:
#
#   /usr/lib/usbridge-agent/usbridge-agent            agent binary
#   /usr/lib/usbridge-agent/usbridge-streamer-launch  KMS launcher, found next
#                                                     to the agent binary by
#                                                     permissions.bundledStreamerLauncher
#   /usr/lib/usbridge-agent/apt-upgrade               run by the agent through
#                                                     pkexec to update itself (internal/update/apply_linux.go)
#   /usr/bin/usbridge-agent                           symlink
#   /usr/share/keyrings/usbridge-archive-keyring.gpg  APT repository key
#   /etc/apt/sources.list.d/usbridge-agent.list       the APT repository: a flat
#                                                     index published as assets of
#                                                     the latest GitHub release
#                                                     (see release-all.yml)
#
# Shipping the source list in the package means a .deb downloaded by hand
# still gets updates through apt, the same way Chrome's .deb does.
set -euo pipefail
umask 022

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
VERSION="$(tr -d ' \t\n\r' < "$REPO_ROOT/VERSION")"
DIST_DIR="$REPO_ROOT/dist/linux"
OUT="$REPO_ROOT/dist/USBridgeAgent-Linux-x86_64-${VERSION}${DEB_SUFFIX:-}.deb"

for f in usbridge-agent usbridge-streamer-launch; do
    [[ -x "$DIST_DIR/$f" ]] || { echo "missing $DIST_DIR/$f — run build_linux.sh first" >&2; exit 1; }
done

ROOT="$DIST_DIR/deb"
rm -rf "$ROOT"
LIB="$ROOT/usr/lib/usbridge-agent"
mkdir -p "$ROOT/DEBIAN" "$LIB" "$ROOT/usr/bin" "$ROOT/usr/share/applications" \
    "$ROOT/usr/share/icons/hicolor/256x256/apps" "$ROOT/usr/share/keyrings" \
    "$ROOT/etc/apt/sources.list.d" "$ROOT/usr/share/doc/usbridge-agent"

install -m 0755 "$DIST_DIR/usbridge-agent" "$LIB/usbridge-agent"
install -m 0755 "$DIST_DIR/usbridge-streamer-launch" "$LIB/usbridge-streamer-launch"
ln -s ../lib/usbridge-agent/usbridge-agent "$ROOT/usr/bin/usbridge-agent"

cat > "$LIB/apt-upgrade" <<'EOF'
#!/bin/sh
# Upgrades usbridge-agent from its own APT repository only. Run as root
# through pkexec by the agent's update dialog.
set -eu
export DEBIAN_FRONTEND=noninteractive
list=/etc/apt/sources.list.d/usbridge-agent.list
apt-get update -o Dir::Etc::sourcelist="$list" -o Dir::Etc::sourceparts=- -o APT::Get::List-Cleanup=0
apt-get install -y --only-upgrade -o Dpkg::Options::=--force-confold usbridge-agent
EOF
chmod 0755 "$LIB/apt-upgrade"

install -m 0644 "$REPO_ROOT/packaging/deb/usbridge-archive-keyring.gpg" "$ROOT/usr/share/keyrings/usbridge-archive-keyring.gpg"
cat > "$ROOT/etc/apt/sources.list.d/usbridge-agent.list" <<'EOF'
# USBridge Agent updates. Remove this file to stop them.
deb [arch=amd64 signed-by=/usr/share/keyrings/usbridge-archive-keyring.gpg] https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download ./
EOF
echo "/etc/apt/sources.list.d/usbridge-agent.list" > "$ROOT/DEBIAN/conffiles"

install -m 0644 "$REPO_ROOT/assets/icons/appicon-256.png" "$ROOT/usr/share/icons/hicolor/256x256/apps/usbridge-agent.png"
cat > "$ROOT/usr/share/applications/usbridge-agent.desktop" <<'EOF'
[Desktop Entry]
Type=Application
Name=USBridge Agent
Comment=Remote access host for USBridge
Exec=usbridge-agent
Icon=usbridge-agent
Terminal=false
Categories=Network;RemoteAccess;
EOF

cat > "$ROOT/usr/share/doc/usbridge-agent/copyright" <<'EOF'
Format: https://www.debian.org/doc/packaging-manuals/copyright-format/1.0/
Upstream-Name: usbridge-agent
Source: https://github.com/USBridge-Technologies/USBridge-Remote
EOF

SIZE_KB="$(du -sk --exclude=DEBIAN "$ROOT" | cut -f1)"
cat > "$ROOT/DEBIAN/control" <<EOF
Package: usbridge-agent
Version: $VERSION
Architecture: amd64
Maintainer: USBridge Technologies <support@usbridge.io>
Installed-Size: $SIZE_KB
Depends: libc6, libgl1, libx11-6
Recommends: pkexec | policykit-1
Section: net
Priority: optional
Homepage: https://www.usbridge.io
Description: USBridge Agent - remote access host
 Streams this computer's desktop to USBridge clients and shares its USB
 devices. Downloads the selected streamer (USBridge Streamer or Sunshine)
 on first run and keeps it up to date.
EOF

rm -f "$OUT"
dpkg-deb --root-owner-group -Zxz --build "$ROOT" "$OUT"
echo "deb: $OUT"
