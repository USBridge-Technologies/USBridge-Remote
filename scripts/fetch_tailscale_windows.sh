#!/usr/bin/env bash
# fetch_tailscale_windows <dest_dir>
#
# Downloads the latest Tailscale Windows MSI, extracts it without installing
# (msiexec /a), and stages the following binaries next to USBridgeAgent.exe:
#
#   tailscale.exe   — Tailscale CLI (optional, for manual VPN management)
#   tailscaled.exe  — Tailscale daemon (optional, system-level VPN)
#   wintun.dll      — WireGuard TUN adapter; required for tsnet to use a real
#                     kernel-mode tunnel instead of slow userspace networking
#
# Env overrides:
#   USBRIDGE_SKIP_TAILSCALE=1     skip bundling entirely (offline/dev builds)
#   USBRIDGE_TAILSCALE_FORCE=1    re-download even if already staged
#   USBRIDGE_TAILSCALE_VERSION=x  pin a version tag (e.g. "1.98.8"); default: latest stable
#   TAILSCALE_ROOT=/path          use local tailscale binaries instead of downloading

fetch_tailscale_windows() {
    local dest="$1"

    if [[ "${USBRIDGE_SKIP_TAILSCALE:-0}" == "1" ]]; then
        echo -e "${YELLOW}USBRIDGE_SKIP_TAILSCALE=1 — skipping Tailscale bundling${NC}"
        return 0
    fi

    # If all three files are already present and force-refresh is not requested, skip.
    if [[ "${USBRIDGE_TAILSCALE_FORCE:-0}" != "1" ]] \
        && [[ -f "$dest/wintun.dll" ]] \
        && [[ -f "$dest/tailscale.exe" ]] \
        && [[ -f "$dest/tailscaled.exe" ]]; then
        echo -e "${GREEN}✓${NC} Tailscale already staged at $dest, skipping download"
        return 0
    fi

    # 1. Try TAILSCALE_ROOT or MSYS2 system paths first
    local _ts_bin_dir=""
    for _candidate in \
        "${TAILSCALE_ROOT:-}" \
        "${TAILSCALE_ROOT:-}/bin" \
        "/ucrt64/bin" \
        "/mingw64/bin" \
        "/clang64/bin"
    do
        [[ -z "$_candidate" ]] && continue
        if [[ -f "$_candidate/wintun.dll" ]] || [[ -f "$_candidate/tailscale.exe" ]]; then
            _ts_bin_dir="$_candidate"
            echo -e "${GREEN}✓${NC} Tailscale found locally: $_ts_bin_dir"
            break
        fi
    done

    # 2. Download from pkgs.tailscale.com if not found locally
    if [[ -z "$_ts_bin_dir" ]]; then
        echo -e "${YELLOW}Tailscale not found locally — downloading...${NC}"

        if ! command -v curl >/dev/null 2>&1; then
            echo -e "${RED}curl not found — cannot download Tailscale${NC}"
            echo "   Set TAILSCALE_ROOT=/path/to/dir/containing/wintun.dll to provide it manually"
            echo "   Or: set USBRIDGE_SKIP_TAILSCALE=1 to skip bundling"
            return 1
        fi

        # Resolve version
        local _ts_version="${USBRIDGE_TAILSCALE_VERSION:-}"
        if [[ -z "$_ts_version" ]]; then
            _ts_version=$(curl -fsSL \
                "https://api.github.com/repos/tailscale/tailscale/releases/latest" \
                2>/dev/null \
                | python -c "import sys,json; d=json.load(sys.stdin); print(d['tag_name'].lstrip('v'))" \
                2>/dev/null || true)
        fi
        [[ -z "$_ts_version" ]] && _ts_version="1.98.8"
        echo "   Tailscale version: $_ts_version"

        local _cache_dir="$REPO_ROOT/.cache/tailscale-windows"
        mkdir -p "$_cache_dir"
        local _msi="$_cache_dir/tailscale-setup-${_ts_version}-amd64.msi"
        local _ts_url="https://pkgs.tailscale.com/stable/tailscale-setup-${_ts_version}-amd64.msi"

        # Use cached MSI if already downloaded
        if [[ ! -f "$_msi" ]]; then
            echo "   Downloading: $_ts_url"
            curl -fL --progress-bar -o "$_msi" "$_ts_url"
        else
            echo -e "${GREEN}✓${NC} Using cached MSI: $_msi"
        fi

        # Extract without installing via msiexec administrative install
        if ! command -v msiexec.exe >/dev/null 2>&1; then
            echo -e "${RED}msiexec.exe not found — cannot extract Tailscale MSI${NC}"
            echo "   Run this script from MSYS2 on Windows, or set TAILSCALE_ROOT manually"
            return 1
        fi

        local _extract_dir="$_cache_dir/extracted"
        rm -rf "$_extract_dir"
        local _msi_win _extract_win
        _msi_win="$(cygpath -w "$_msi" 2>/dev/null || echo "$_msi")"
        _extract_win="$(cygpath -w "$_extract_dir" 2>/dev/null || echo "$_extract_dir")"

        echo "   Extracting MSI (no installation)..."
        powershell -NoProfile -NonInteractive -Command \
            "\$p = Start-Process msiexec.exe -ArgumentList '/a \"$_msi_win\" /qn TARGETDIR=\"$_extract_win\"' -Wait -PassThru -WindowStyle Hidden; exit \$p.ExitCode"

        local _found_exe
        _found_exe="$(find "$_extract_dir" -name "tailscale.exe" 2>/dev/null | head -1)"
        if [[ -z "$_found_exe" ]]; then
            echo -e "${RED}tailscale.exe not found in extracted MSI${NC}"
            return 1
        fi
        _ts_bin_dir="$(dirname "$_found_exe")"
        echo -e "${GREEN}✓${NC} Tailscale extracted to $_ts_bin_dir"
    fi

    # 3. Copy binaries to dest
    mkdir -p "$dest"
    local _copied=0
    for _f in wintun.dll tailscale.exe tailscaled.exe; do
        local _src="$_ts_bin_dir/$_f"
        if [[ -f "$_src" ]]; then
            cp -L "$_src" "$dest/"
            echo -e "   ${GREEN}✓${NC} $_f"
            _copied=$(( _copied + 1 ))
        elif [[ "$_f" == "wintun.dll" ]]; then
            echo -e "   ${RED}❌ wintun.dll not found in $_ts_bin_dir${NC}"
        else
            echo -e "   ${YELLOW}⚠${NC} $_f not found in $_ts_bin_dir"
        fi
    done

    echo -e "${GREEN}✓${NC} Tailscale: $_copied file(s) staged at $dest"
}
