#!/usr/bin/env bash
# Shared helper: obtain the official Sunshine (LizardByte/Sunshine) build and
# stage it next to the agent binary. Sunshine is the Moonlight GameStream
# host — it owns all video/audio/input capture for the Moonlight/Sunshine
# protocol; usbridge_agent only pairs with it and relays PINs.
#
# Linux is built from source (not the prebuilt AppImage): Sunshine's AppImage
# and Flatpak builds intentionally refuse KMS capture at runtime
# (-DSUNSHINE_BUILD_APPIMAGE=ON gates it), which breaks the agent's root/KMS
# capture-mode option. A native build has no such restriction. Windows/macOS
# use the official prebuilt release — DXGI/ScreenCaptureKit have no AppImage-
# style packaging restriction, so there's nothing to gain from compiling.
#
# Env overrides:
#   USBRIDGE_SKIP_SUNSHINE=1     skip bundling Sunshine entirely (offline/dev builds)
#   USBRIDGE_SUNSHINE_FORCE=1    rebuild/re-download even if already staged
#   USBRIDGE_SUNSHINE_VERSION=x  pin a release tag instead of "latest"
#   USBRIDGE_SUNSHINE_CUDA=1     (Linux only) build with Nvidia CUDA/NVENC support

_sunshine_repo="LizardByte/Sunshine"

_sunshine_require() {
    if ! command -v "$1" >/dev/null 2>&1; then
        echo -e "${RED}Missing dependency: $1 (needed to fetch Sunshine)${NC}"
        echo "$2"
        exit 1
    fi
}

_sunshine_asset_url() {
    local asset_name="$1"
    local version="${USBRIDGE_SUNSHINE_VERSION:-latest}"
    local api_url
    if [[ "$version" == "latest" ]]; then
        api_url="https://api.github.com/repos/${_sunshine_repo}/releases/latest"
    else
        api_url="https://api.github.com/repos/${_sunshine_repo}/releases/tags/${version}"
    fi
    curl -fsSL "$api_url" | python3 -c "
import sys, json
data = json.load(sys.stdin)
for a in data.get('assets', []):
    if a['name'] == '${asset_name}':
        print(a['browser_download_url'])
        break
"
}

_sunshine_resolve_tag() {
    local version="${USBRIDGE_SUNSHINE_VERSION:-}"
    if [[ -n "$version" ]]; then
        echo "$version"
        return 0
    fi
    curl -fsSL "https://api.github.com/repos/${_sunshine_repo}/releases/latest" \
        | python3 -c "import sys, json; print(json.load(sys.stdin)['tag_name'])"
}

# build_sunshine_linux
# Clones LizardByte/Sunshine and builds+installs it natively (system-wide,
# via a real .deb) using its own scripts/linux_build.sh — deliberately
# WITHOUT --appimage-build, since that flag compiles in a runtime check that
# refuses KMS capture, AND bakes the web-UI assets path in as relative to the
# AppImage bundle. A non-AppImage build compiles the assets path in as the
# *absolute* install prefix (/usr/share/sunshine), so — unlike the other
# platforms — this can't be kept self-contained under dist/: Sunshine must
# actually be installed at that path. We build a .deb and install it with
# apt so dependency resolution and the postinst (which sets CAP_SYS_ADMIN
# for KMS and reloads udev rules for /dev/uinput /dev/uhid) both run
# normally, matching how Sunshine expects to be deployed.
build_sunshine_linux() {
    if [[ "${USBRIDGE_SKIP_SUNSHINE:-0}" == "1" ]]; then
        echo -e "${YELLOW}USBRIDGE_SKIP_SUNSHINE=1 — skipping Sunshine build${NC}"
        return 0
    fi
    if command -v sunshine >/dev/null 2>&1 && [[ "${USBRIDGE_SUNSHINE_FORCE:-0}" != "1" ]]; then
        echo -e "${GREEN}✓${NC} sunshine already installed ($(command -v sunshine)), skipping build"
        return 0
    fi

    _sunshine_require git "Install with: sudo apt install git"
    _sunshine_require cmake "Install with: sudo apt install cmake"
    _sunshine_require curl "Install with: sudo apt install curl"
    _sunshine_require python3 "Install with: sudo apt install python3"
    _sunshine_require sudo "linux_build.sh needs sudo to install build deps and the resulting package"

    local tag
    tag="$(_sunshine_resolve_tag)"
    if [[ -z "$tag" ]]; then
        echo -e "${RED}Failed to resolve Sunshine version to build${NC}"
        exit 1
    fi

    local src_dir
    src_dir="$(mktemp -d)"
    echo -e "${YELLOW}Cloning LizardByte/Sunshine @ ${tag}...${NC}"
    git clone --depth 1 --branch "$tag" --recurse-submodules --shallow-submodules \
        "https://github.com/${_sunshine_repo}.git" "$src_dir"

    local build_args=(
        --publisher-name='usbridge_agent'
        --publisher-website='https://github.com/itsme228/usbridge_agent'
        --publisher-issue-url='https://github.com/itsme228/usbridge_agent/issues'
        --skip-cleanup
    )
    if [[ "${USBRIDGE_SUNSHINE_CUDA:-0}" != "1" ]]; then
        build_args+=(--skip-cuda)
    fi

    echo -e "${YELLOW}Building Sunshine from source (native, KMS-capable).${NC}"
    echo -e "${YELLOW}This installs build dependencies via sudo and can take 15-60+ minutes.${NC}"
    (cd "$src_dir" && chmod +x scripts/linux_build.sh && ./scripts/linux_build.sh "${build_args[@]}")
    local build_status=$?
    if [[ $build_status -ne 0 ]]; then
        rm -rf "$src_dir"
        echo -e "${RED}Sunshine build failed${NC}"
        exit 1
    fi

    local deb_file
    deb_file="$(find "$src_dir" -name '*.deb' | head -1)"
    if [[ -z "$deb_file" ]]; then
        rm -rf "$src_dir"
        echo -e "${RED}Sunshine build succeeded but no .deb package was produced${NC}"
        exit 1
    fi

    echo -e "${YELLOW}Installing $(basename "$deb_file")...${NC}"
    sudo apt-get install -y "$deb_file"
    rm -rf "$src_dir"

    if ! command -v sunshine >/dev/null 2>&1; then
        echo -e "${RED}Sunshine package installed but 'sunshine' not found on PATH${NC}"
        exit 1
    fi
    echo -e "${GREEN}✓${NC} Sunshine installed at $(command -v sunshine)"
}

# fetch_sunshine_windows <dest_dir>
fetch_sunshine_windows() {
    local dest="$1"
    if [[ "${USBRIDGE_SKIP_SUNSHINE:-0}" == "1" ]]; then
        echo -e "${YELLOW}USBRIDGE_SKIP_SUNSHINE=1 — skipping Sunshine bundling${NC}"
        return 0
    fi
    if [[ -f "$dest/sunshine.exe" && "${USBRIDGE_SUNSHINE_FORCE:-0}" != "1" ]]; then
        echo -e "${GREEN}✓${NC} Sunshine already staged at $dest, skipping download"
        return 0
    fi

    _sunshine_require curl "Install with: pacman -S --needed curl"
    _sunshine_require python "Install with: pacman -S --needed mingw-w64-ucrt-x86_64-python"

    echo -e "${YELLOW}Fetching Sunshine (Moonlight GameStream host)...${NC}"
    local url
    url="$(_sunshine_asset_url "Sunshine-Windows-AMD64-portable.zip")"
    if [[ -z "$url" ]]; then
        echo -e "${RED}Failed to resolve Sunshine Windows download URL${NC}"
        exit 1
    fi

    local tmp_zip
    tmp_zip="$(mktemp).zip"
    echo "Downloading: $url"
    curl -fL --progress-bar -o "$tmp_zip" "$url"

    rm -rf "$dest"
    mkdir -p "$dest"
    python -m zipfile -e "$tmp_zip" "$dest"
    rm -f "$tmp_zip"

    echo -e "${GREEN}✓${NC} Sunshine staged at $dest"
}

# fetch_sunshine_macos <dest_dir>
fetch_sunshine_macos() {
    local dest="$1"
    if [[ "${USBRIDGE_SKIP_SUNSHINE:-0}" == "1" ]]; then
        echo -e "${YELLOW}USBRIDGE_SKIP_SUNSHINE=1 — skipping Sunshine bundling${NC}"
        return 0
    fi
    if [[ -d "$dest/Sunshine.app" && "${USBRIDGE_SUNSHINE_FORCE:-0}" != "1" ]]; then
        echo -e "${GREEN}✓${NC} Sunshine already staged at $dest, skipping download"
        return 0
    fi

    _sunshine_require curl "Install Xcode Command Line Tools: xcode-select --install"
    _sunshine_require python3 "Install Xcode Command Line Tools: xcode-select --install"

    local arch
    arch="$(uname -m)"
    local asset_name="Sunshine-macOS-x86_64.dmg"
    if [[ "$arch" == "arm64" ]]; then
        asset_name="Sunshine-macOS-arm64.dmg"
    fi

    echo -e "${YELLOW}Fetching Sunshine (Moonlight GameStream host)...${NC}"
    local url
    url="$(_sunshine_asset_url "$asset_name")"
    if [[ -z "$url" ]]; then
        echo -e "${RED}Failed to resolve Sunshine macOS download URL${NC}"
        exit 1
    fi

    local tmp_dmg
    tmp_dmg="$(mktemp).dmg"
    echo "Downloading: $url"
    curl -fL --progress-bar -o "$tmp_dmg" "$url"

    local mount_point
    mount_point="$(mktemp -d)"
    hdiutil attach -nobrowse -quiet -mountpoint "$mount_point" "$tmp_dmg"

    rm -rf "$dest"
    mkdir -p "$dest"
    cp -R "$mount_point"/*.app "$dest/Sunshine.app"

    hdiutil detach -quiet "$mount_point"
    rm -f "$tmp_dmg"

    echo -e "${GREEN}✓${NC} Sunshine staged at $dest/Sunshine.app"
}
