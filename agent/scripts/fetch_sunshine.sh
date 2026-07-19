#!/usr/bin/env bash
# Shared helper: obtain the usbridge fork of Sunshine and stage it next to the
# agent binary. Sunshine is the Moonlight GameStream host — it owns all
# video/audio/input capture for the Moonlight/Sunshine protocol; usbridge_agent
# pairs with it and relays PINs.
#
# We use our own fork (itsme228/Sunshine) instead of the upstream LizardByte
# release because it adds web_bind_address: a config key that lets the HTTPS
# admin UI bind to a separate address (127.0.0.1) while streaming ports stay
# bound to the VPN/LAN interface via bind_address.
#
# macOS and Linux are built from source (the fork's master branch).
# Windows still uses a prebuilt release from upstream (the feature is not
# strictly needed on Windows where Moonlight normally connects over LAN).
#
# Env overrides:
#   USBRIDGE_SKIP_SUNSHINE=1     skip bundling Sunshine entirely (offline/dev builds)
#   USBRIDGE_SUNSHINE_FORCE=1    rebuild/re-download even if already staged
#   USBRIDGE_SUNSHINE_VERSION=x  pin a release tag instead of "latest"
#   USBRIDGE_SUNSHINE_CUDA=1     (Linux only) build with Nvidia CUDA/NVENC support

_sunshine_repo="itsme228/Sunshine"

# Принудительно устанавливаем нужную версию и заставляем скрипт обновлять бинарники
export USBRIDGE_SUNSHINE_VERSION="${USBRIDGE_SUNSHINE_VERSION:-v2026.719.3.usbridge}"
export USBRIDGE_SUNSHINE_FORCE="${USBRIDGE_SUNSHINE_FORCE:-1}"

_sunshine_require() {
    if ! command -v "$1" >/dev/null 2>&1; then
        echo -e "${RED}Missing dependency: $1 (needed to fetch Sunshine)${NC}"
        echo "$2"
        exit 1
    fi
}

# _sunshine_clean_creds <dir>
# Removes Sunshine credential/state files that may have been written by a
# previous run of Sunshine from the dist directory. These files contain device
# pairing keys and admin password hashes that must never be shipped in a
# distribution archive.
_sunshine_clean_creds() {
    local dir="$1"
    for _f in sunshine_state.json credentials.json creds.json; do
        if [[ -f "$dir/$_f" ]]; then
            rm -f "$dir/$_f"
            echo -e "  ${YELLOW}Removed Sunshine credential file: $_f${NC}"
        fi
    done
}

# GitHub's unauthenticated API rate limit (60/hour/IP) is easy to exhaust on
# shared Actions runner IP ranges, especially with several jobs querying it
# in the same workflow run. Authenticate with GITHUB_TOKEN/GH_TOKEN (already
# available in every Actions job) when present to get the 5000/hour limit
# instead — a silent rate-limit failure here looks identical to "no release
# asset yet" and falls through to a 10-60 min source build.
_sunshine_curl_auth=()
if [[ -n "${GITHUB_TOKEN:-${GH_TOKEN:-}}" ]]; then
    _sunshine_curl_auth=(-H "Authorization: Bearer ${GITHUB_TOKEN:-$GH_TOKEN}")
fi

_sunshine_asset_url() {
    local asset_name="$1"
    local version="${USBRIDGE_SUNSHINE_VERSION:-latest}"
    local api_url
    if [[ "$version" == "latest" ]]; then
        api_url="https://api.github.com/repos/${_sunshine_repo}/releases/latest"
    else
        api_url="https://api.github.com/repos/${_sunshine_repo}/releases/tags/${version}"
    fi
    # Use || true so a 404 (no release yet) returns empty string instead of aborting.
    # "${arr[@]+"${arr[@]}"}" (not "${arr[@]}") — macOS ships bash 3.2, where
    # expanding an empty array under `set -u` is an unbound-variable error
    # (fixed only in bash 4.4+); this form is the standard 3.2-safe idiom.
    curl -fsSL "${_sunshine_curl_auth[@]+"${_sunshine_curl_auth[@]}"}" "$api_url" 2>/dev/null | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    for a in data.get('assets', []):
        if a['name'] == '${asset_name}':
            print(a['browser_download_url'])
            break
except Exception:
    pass
" 2>/dev/null || true
}

_sunshine_resolve_tag() {
    local version="${USBRIDGE_SUNSHINE_VERSION:-}"
    if [[ -n "$version" ]]; then
        echo "$version"
        return 0
    fi
    curl -fsSL "${_sunshine_curl_auth[@]+"${_sunshine_curl_auth[@]}"}" "https://api.github.com/repos/${_sunshine_repo}/releases/latest" \
        | python3 -c "import sys, json; print(json.load(sys.stdin)['tag_name'])"
}

# fetch_sunshine_linux / build_sunshine_linux <dest_dir>
# Stages itsme228/Sunshine (our fork with web_bind_address patch) under
# $dest_dir as a cmake install tree: $dest_dir/usr/bin/sunshine and
# $dest_dir/usr/local/assets/. Built with SUNSHINE_BUILD_APPIMAGE=ON so
# the binary uses relative asset paths (./usr/local/assets relative to cwd),
# making it relocatable and compatible with being bundled in the agent AppImage.
# No system-wide install — the agent binary is responsible for launching it.
# Fast path: download pre-built tarball from fork's GitHub Releases.
# Slow path (fallback): clone fork and build from source with cmake.
fetch_sunshine_linux() { build_sunshine_linux "$@"; }
build_sunshine_linux() {
    local dest="$1"
    if [[ "${USBRIDGE_SKIP_SUNSHINE:-0}" == "1" ]]; then
        echo -e "${YELLOW}USBRIDGE_SKIP_SUNSHINE=1 — skipping Sunshine bundling${NC}"
        return 0
    fi
    if [[ -f "$dest/usr/bin/sunshine" && "${USBRIDGE_SUNSHINE_FORCE:-0}" != "1" ]]; then
        echo -e "${GREEN}✓${NC} Sunshine already staged at $dest, skipping"
        _sunshine_clean_creds "$dest"
        return 0
    fi

    _sunshine_require curl "Install with: sudo apt install curl"
    _sunshine_require python3 "Install with: sudo apt install python3"

    local arch
    arch="$(uname -m)"
    local asset_name="Sunshine-Linux-x86_64.tar.gz"
    [[ "$arch" == "aarch64" ]] && asset_name="Sunshine-Linux-aarch64.tar.gz"

    # Fast path: download pre-built tarball from our fork's releases.
    echo -e "${YELLOW}Fetching Sunshine fork (itsme228/Sunshine, web_bind_address patch)...${NC}"
    local url
    url="$(_sunshine_asset_url "$asset_name")"

    if [[ -n "$url" ]]; then
        local tmp_tgz
        tmp_tgz="$(mktemp).tar.gz"
        echo "Downloading: $url"
        curl -fL --progress-bar -o "$tmp_tgz" "$url"
        rm -rf "$dest"
        mkdir -p "$dest"
        tar -xzf "$tmp_tgz" -C "$dest"
        rm -f "$tmp_tgz"
        chmod +x "$dest/usr/bin/sunshine" 2>/dev/null || true
        _sunshine_clean_creds "$dest"
        echo -e "${GREEN}✓${NC} Sunshine (fork release) staged at $dest"
        return 0
    fi

    # Slow path: no release yet — build from source.
    echo -e "${YELLOW}No fork release found — building Sunshine from source (15-60+ min)...${NC}"
    _sunshine_require git "Install with: sudo apt install git"
    _sunshine_require cmake "Install with: sudo apt install cmake"
    _sunshine_require sudo "cmake install needs sudo for build deps"

    local src_dir
    src_dir="$(mktemp -d)"
    git clone --depth 1 --recurse-submodules --shallow-submodules \
        "https://github.com/${_sunshine_repo}.git" "$src_dir"

    local cuda_flag="OFF"
    [[ "${USBRIDGE_SUNSHINE_CUDA:-0}" == "1" ]] && cuda_flag="ON"

    cmake \
        -B "$src_dir/build" -S "$src_dir" \
        -DCMAKE_BUILD_TYPE=Release \
        -DBUILD_DOCS=OFF \
        -DSUNSHINE_BUILD_APPIMAGE=ON \
        -DSUNSHINE_ENABLE_TRAY=OFF \
        -DSUNSHINE_ENABLE_CUDA="$cuda_flag" \
        -DSUNSHINE_PUBLISHER_NAME="usbridge_agent" \
        -DSUNSHINE_PUBLISHER_WEBSITE="https://github.com/itsme228/usbridge_agent" \
        -DSUNSHINE_PUBLISHER_ISSUE_URL="https://github.com/itsme228/usbridge_agent/issues"

    cmake --build "$src_dir/build" -j "$(nproc 2>/dev/null || sysctl -n hw.ncpu)"

    rm -rf "$dest"
    DESTDIR="$dest" cmake --install "$src_dir/build" --prefix /usr
    rm -rf "$src_dir"
    chmod +x "$dest/usr/bin/sunshine" 2>/dev/null || true

    _sunshine_clean_creds "$dest"
    echo -e "${GREEN}✓${NC} Sunshine (fork source build) staged at $dest"
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
        _sunshine_clean_creds "$dest"
        return 0
    fi

    _sunshine_require curl "Install with: pacman -S --needed curl"
    _sunshine_require python "Install with: pacman -S --needed mingw-w64-ucrt-x86_64-python"

    echo -e "${YELLOW}Fetching Sunshine fork (itsme228/Sunshine, web_bind_address patch)...${NC}"
    local url
    url="$(_sunshine_asset_url "Sunshine-Windows-x86_64-portable.zip")"
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

    # The portable zip places everything under a top-level Sunshine/ subdirectory.
    # Flatten it so sunshine.exe lives directly under $dest (matching BinaryPath in Go).
    if [[ -f "$dest/Sunshine/sunshine.exe" ]]; then
        mv "$dest/Sunshine/"* "$dest/"
        rmdir "$dest/Sunshine" 2>/dev/null || true
    fi

    _sunshine_clean_creds "$dest"
    echo -e "${GREEN}✓${NC} Sunshine staged at $dest"
}

# fetch_sunshine_macos / build_sunshine_macos <dest_dir>
# Stages itsme228/Sunshine (our fork with web_bind_address patch).
# Fast path: download pre-built DMG from fork's GitHub Releases.
# Slow path (fallback): clone fork and build from source via CMake.
fetch_sunshine_macos() { build_sunshine_macos "$@"; }
build_sunshine_macos() {
    local dest="$1"
    if [[ "${USBRIDGE_SKIP_SUNSHINE:-0}" == "1" ]]; then
        echo -e "${YELLOW}USBRIDGE_SKIP_SUNSHINE=1 — skipping Sunshine bundling${NC}"
        return 0
    fi
    if [[ -d "$dest/Sunshine.app" && "${USBRIDGE_SUNSHINE_FORCE:-0}" != "1" ]]; then
        echo -e "${GREEN}✓${NC} Sunshine already staged at $dest, skipping"
        _sunshine_clean_creds "$dest"
        return 0
    fi

    _sunshine_require curl "Install Xcode Command Line Tools: xcode-select --install"
    _sunshine_require python3 "Install Xcode Command Line Tools: xcode-select --install"

    local arch
    arch="$(uname -m)"
    local asset_name="Sunshine-macOS-arm64.dmg"
    [[ "$arch" != "arm64" ]] && asset_name="Sunshine-macOS-x86_64.dmg"

    # Fast path: download pre-built DMG from our fork's releases.
    echo -e "${YELLOW}Fetching Sunshine fork (itsme228/Sunshine, web_bind_address patch)...${NC}"
    local url
    url="$(_sunshine_asset_url "$asset_name")"

    if [[ -n "$url" ]]; then
        local tmp_dmg
        tmp_dmg="$(mktemp).dmg"
        echo "Downloading: $url"
        curl -fL --progress-bar -o "$tmp_dmg" "$url"

        local mount_point
        mount_point="$(mktemp -d)"
        echo "y" | hdiutil attach -nobrowse -mountpoint "$mount_point" "$tmp_dmg" >/dev/null

        rm -rf "$dest"
        mkdir -p "$dest"
        cp -R "$mount_point"/*.app "$dest/Sunshine.app"

        hdiutil detach -quiet "$mount_point"
        rm -f "$tmp_dmg"

        xattr -dr com.apple.quarantine "$dest/Sunshine.app" 2>/dev/null || true
        _sunshine_clean_creds "$dest"
        echo -e "${GREEN}✓${NC} Sunshine (fork release) staged at $dest/Sunshine.app"
        return 0
    fi

    # Slow path: no release yet — build from source.
    echo -e "${YELLOW}No fork release found — building Sunshine from source (10-30 min)...${NC}"
    _sunshine_require cmake "Install with: brew install cmake"
    _sunshine_require git "Install Xcode Command Line Tools: xcode-select --install"
    _sunshine_require brew "Install Homebrew: https://brew.sh"
    _sunshine_require node "Install with: brew install node"

    brew install --quiet cmake node pkgconf "icu4c@78" miniupnpc "openssl@3" opus \
        2>&1 | grep -v "already installed" || true

    local src_dir
    src_dir="$(mktemp -d)"
    git clone --depth 1 --recurse-submodules --shallow-submodules \
        "https://github.com/${_sunshine_repo}.git" "$src_dir"

    cmake \
        -B "$src_dir/build" -S "$src_dir" \
        -DCMAKE_BUILD_TYPE=Release \
        -DBUILD_DOCS=OFF \
        -DICU_ROOT="$(brew --prefix "icu4c@78" 2>/dev/null)" \
        -DOPENSSL_ROOT_DIR="$(brew --prefix "openssl@3" 2>/dev/null)" \
        -DOpus_ROOT_DIR="$(brew --prefix opus 2>/dev/null)" \
        -DSUNSHINE_PUBLISHER_NAME="usbridge_agent" \
        -DSUNSHINE_PUBLISHER_WEBSITE="https://github.com/itsme228/usbridge_agent" \
        -DSUNSHINE_PUBLISHER_ISSUE_URL="https://github.com/itsme228/usbridge_agent/issues"

    cmake --build "$src_dir/build" -j "$(sysctl -n hw.ncpu)"
    (cd "$src_dir/build" && cpack -G DragNDrop --config CPackConfig.cmake)

    local dmg_file
    dmg_file="$(find "$src_dir/build" -name "*.dmg" | head -1)"
    if [[ -z "$dmg_file" ]]; then
        rm -rf "$src_dir"
        echo -e "${RED}Sunshine source build failed — no .dmg produced${NC}"
        exit 1
    fi

    local mount_point
    mount_point="$(mktemp -d)"
    echo "y" | hdiutil attach -nobrowse -mountpoint "$mount_point" "$dmg_file" >/dev/null

    rm -rf "$dest"
    mkdir -p "$dest"
    cp -R "$mount_point"/*.app "$dest/Sunshine.app"

    hdiutil detach -quiet "$mount_point"
    rm -rf "$src_dir"

    xattr -dr com.apple.quarantine "$dest/Sunshine.app" 2>/dev/null || true
    _sunshine_clean_creds "$dest"
    echo -e "${GREEN}✓${NC} Sunshine (fork source build) staged at $dest/Sunshine.app"
}
