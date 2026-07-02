#!/usr/bin/env bash
# Shared helper: download the official Sunshine (LizardByte/Sunshine) release
# and stage it next to the agent binary. Sunshine is the Moonlight GameStream
# host — it owns all video/audio/input capture for the Moonlight/Sunshine
# protocol; usbridge_agent only pairs with it and relays PINs.
#
# Env overrides:
#   USBRIDGE_SKIP_SUNSHINE=1     skip bundling Sunshine entirely (offline/dev builds)
#   USBRIDGE_SUNSHINE_FORCE=1    re-download even if already staged
#   USBRIDGE_SUNSHINE_VERSION=x  pin a release tag instead of "latest"

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

# fetch_sunshine_linux <dest_dir>
# Downloads the portable AppImage and extracts it (--appimage-extract needs no
# FUSE, so it works in containers/CI too) so the result runs without a FUSE
# runtime installed on the target machine.
fetch_sunshine_linux() {
    local dest="$1"
    if [[ "${USBRIDGE_SKIP_SUNSHINE:-0}" == "1" ]]; then
        echo -e "${YELLOW}USBRIDGE_SKIP_SUNSHINE=1 — skipping Sunshine bundling${NC}"
        return 0
    fi
    if [[ -x "$dest/AppRun" && "${USBRIDGE_SUNSHINE_FORCE:-0}" != "1" ]]; then
        echo -e "${GREEN}✓${NC} Sunshine already staged at $dest, skipping download"
        return 0
    fi

    _sunshine_require curl "Install with: sudo apt install curl"
    _sunshine_require python3 "Install with: sudo apt install python3"

    echo -e "${YELLOW}Fetching Sunshine (Moonlight GameStream host)...${NC}"
    local url
    url="$(_sunshine_asset_url "sunshine.AppImage")"
    if [[ -z "$url" ]]; then
        echo -e "${RED}Failed to resolve Sunshine AppImage download URL${NC}"
        exit 1
    fi

    local tmp_dir
    tmp_dir="$(mktemp -d)"
    local appimage="$tmp_dir/sunshine.AppImage"
    echo "Downloading: $url"
    curl -fL --progress-bar -o "$appimage" "$url"
    chmod +x "$appimage"

    rm -rf "$dest"
    mkdir -p "$dest"
    (cd "$tmp_dir" && "./sunshine.AppImage" --appimage-extract >/dev/null)
    mv "$tmp_dir/squashfs-root"/* "$dest/"
    rm -rf "$tmp_dir"

    echo -e "${GREEN}✓${NC} Sunshine staged at $dest/AppRun"
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
