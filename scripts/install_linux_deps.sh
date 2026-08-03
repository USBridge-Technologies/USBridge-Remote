#!/usr/bin/env bash
# Installs the apt packages needed to build USBridge (agent and/or client) on
# Debian/Ubuntu. Mirrors the "Install Linux build deps" steps from
# .github/workflows/{agent-build,agent-release,release-all}.yml so a local
# build matches what CI installs.
#
# Usage:
#   ./scripts/install_linux_deps.sh            # everything (agent + client)
#   ./scripts/install_linux_deps.sh agent       # agent build deps only
#   ./scripts/install_linux_deps.sh client      # client build deps only
#
# Runs the actual installation as root. If not already root, re-execs itself
# under sudo, so it's fine to run directly as your normal user.

set -euo pipefail

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

SCOPE="${1:-all}"
case "$SCOPE" in
    all|agent|client) ;;
    *)
        echo -e "${RED}Unknown scope: $SCOPE${NC} (expected: all|agent|client)"
        exit 1
        ;;
esac

if [[ "$EUID" -ne 0 ]]; then
    echo -e "${YELLOW}Re-running as root via sudo...${NC}"
    exec sudo -E bash "$0" "$SCOPE"
fi

if ! command -v apt-get >/dev/null 2>&1; then
    echo -e "${RED}apt-get not found — this script only supports Debian/Ubuntu.${NC}"
    exit 1
fi

# Refresh the package index before any apt-cache lookups below (a fresh
# install's cache can otherwise be empty and make pkexec look unavailable
# when it isn't).
apt-get update

# Base tooling shared by every build path (cgo compiler, pkg-config for
# go-gl/cmake package discovery, git+cmake+curl+python3 for fetch_sunshine.sh
# and build_moonlight.sh source fallbacks).
PKGS=(build-essential pkg-config git cmake curl python3)

# Runtime dependency for the agent's permission-request flow (not just the
# build): RequestAccessibility/RequestKMSCapture in
# agent/internal/permissions/service_linux.go shell out to `pkexec` to
# install the uinput udev rule / setcap the KMS capture launcher. Ubuntu's
# desktop images pull this in via the `policykit-1` metapackage, but Debian
# split the pkexec binary into its own package (and a stock Debian install
# doesn't put the user in the sudo group or start a polkit agent for you the
# way Ubuntu does) — so `pkexec` can be missing even though polkitd itself is
# running. Try the modern package name first, fall back to the metapackage.
if apt-cache show pkexec >/dev/null 2>&1; then
    POLKIT_PKG=pkexec
else
    POLKIT_PKG=policykit-1
fi

# agent: Fyne GUI (go-gl/glfw) needs GL + X11 + XKB headers. See
# .github/workflows/agent-build.yml "Install Linux build deps (Fyne/X11)".
AGENT_PKGS=(
    libgl1-mesa-dev xorg-dev libxkbcommon-dev
    # Sunshine runtime shared libs, needed so linuxdeploy can resolve them
    # while bundling the AppImage. See agent-release.yml / release-all.yml.
    libminiupnpc-dev libva2 libva-drm2 libvdpau1
    libpulse0 libopus0 libcap2 libdrm2 libevdev2
    libxtst6 libxrandr2 libxfixes3 libnuma1 libcurl4
    libayatana-appindicator3-1 libssl3
    libpipewire-0.3-0 libwayland-client0 libwayland-egl1
    # `setcap`/`getcap` (KMSCaptureGranted/RequestKMSCapture) and `pkexec`
    # (both permission requests) — see comment above.
    libcap2-bin "$POLKIT_PKG"
    # rust-shine's vulkan-tonemap crate shells out to glslangValidator at
    # build time to compile its compute shaders to SPIR-V. Needed only for
    # `build_linux.sh -streamer rustshine`. See
    # rust-shine/crates/vulkan-tonemap/build.rs. rust-shine itself is built
    # with cargo/rustup, which this script does not install — see
    # https://rustup.rs.
    glslang-tools
    # rust-shine's v4l2-sys-mit crate uses bindgen at build time, which needs
    # libclang.so to parse C headers.
    libclang-dev
)

# client: Fyne GUI + Moonlight HW decode/core libs. See release-all.yml
# "Install Linux build deps" (client job) and client/scripts/build_linux.sh.
CLIENT_PKGS=(
    libgl1-mesa-dev xorg-dev libxkbcommon-dev
    libavcodec-dev libavutil-dev libswscale-dev libpulse-dev
    libopus-dev libssl-dev libvulkan-dev
    libgstreamer1.0-dev libgstreamer-plugins-base1.0-dev
)

case "$SCOPE" in
    agent)  PKGS+=("${AGENT_PKGS[@]}") ;;
    client) PKGS+=("${CLIENT_PKGS[@]}") ;;
    all)    PKGS+=("${AGENT_PKGS[@]}" "${CLIENT_PKGS[@]}") ;;
esac

# Dedupe while preserving order.
declare -A seen
UNIQUE_PKGS=()
for p in "${PKGS[@]}"; do
    if [[ -z "${seen[$p]:-}" ]]; then
        seen[$p]=1
        UNIQUE_PKGS+=("$p")
    fi
done

echo -e "${YELLOW}Installing (${SCOPE}): ${UNIQUE_PKGS[*]}${NC}"
apt-get install -y "${UNIQUE_PKGS[@]}"

echo -e "${GREEN}✓${NC} Linux build deps installed (scope: $SCOPE)"
