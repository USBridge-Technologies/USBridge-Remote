#!/usr/bin/env bash
# Builds the private itsme228/rust-shine gamestream-server binary from an
# EXISTING local checkout (this script never clones — the private repo is
# never referenced by URL here, only via a path the caller already has) and
# stages it next to the agent, mirroring fetch_sunshine.sh's staging layout
# (dest_dir/gamestream-server[.exe]) so streamhost.rustshineBackend's
# BinaryPath() finds it.
#
# Set RUSTSHINE_SRC_DIR to the local checkout path. Defaults to a sibling
# "rust-shine" directory next to this repo's own checkout (i.e.
# ../rust-shine relative to the repo root), matching a typical side-by-side
# clone layout. Anyone without their own checkout of the private repo simply
# won't have this directory, and fetch_rustshine_* below fails with a clear
# message instead of silently doing nothing — -streamer rustshine is not
# meant to work without access to that private source.
set -euo pipefail

_rustshine_src_dir() {
    # REPO_ROOT here is agent/ (agent/scripts/build_*.sh set REPO_ROOT to
    # $SCRIPT_DIR/.., i.e. the agent subdirectory, not the repo checkout
    # root) — so the sibling-of-the-checkout default is two levels up.
    echo "${RUSTSHINE_SRC_DIR:-$REPO_ROOT/../../rust-shine}"
}

# _build_rustshine_binary <dest_dir>
# Builds bin/gamestream-server in release mode from RUSTSHINE_SRC_DIR and
# copies the resulting binary into dest_dir. Common to all three OSes: on
# Linux/macOS this is a native `cargo build`; on Windows this script is
# invoked from inside MSYS2 UCRT64, which is also a native (not
# cross-compiled) target for a Rust toolchain installed there, so the same
# plain `cargo build --release` applies.
_build_rustshine_binary() {
    local dest_dir="$1"
    local src_dir
    src_dir="$(_rustshine_src_dir)"

    if [[ ! -d "$src_dir" ]]; then
        echo -e "${RED}RUSTSHINE_SRC_DIR not found: $src_dir${NC}" >&2
        echo "This is the private itsme228/rust-shine checkout — set RUSTSHINE_SRC_DIR" >&2
        echo "to its path, or clone it there yourself if you have access." >&2
        exit 1
    fi

    if command -v git >/dev/null 2>&1 && [[ -d "$src_dir/.git" ]]; then
        echo -e "${YELLOW}Refreshing rust-shine checkout (git pull --ff-only)...${NC}"
        git -C "$src_dir" pull --ff-only 2>&1 | sed 's/^/   /' || \
            echo -e "   ${YELLOW}⚠${NC} git pull failed/skipped — building whatever is currently checked out"
    fi

    if ! command -v cargo >/dev/null 2>&1; then
        echo -e "${RED}cargo not found in PATH — cannot build rust-shine${NC}" >&2
        exit 1
    fi

    echo -e "${YELLOW}Building gamestream-server (release) from $src_dir...${NC}"
    ( cd "$src_dir" && cargo build --release --features desktop -p gamestream-server )

    local bin_name="gamestream-server"
    [[ "${GOOS:-}" == "windows" ]] && bin_name="gamestream-server.exe"
    local built="$src_dir/target/release/$bin_name"
    if [[ ! -f "$built" ]]; then
        echo -e "${RED}Build succeeded but $built not found${NC}" >&2
        exit 1
    fi

    mkdir -p "$dest_dir"
    cp -L "$built" "$dest_dir/$bin_name"
    chmod +x "$dest_dir/$bin_name" 2>/dev/null || true
    echo -e "${GREEN}✓${NC} gamestream-server staged at $dest_dir/$bin_name"
}

fetch_rustshine_linux() { _build_rustshine_binary "$1"; }
fetch_rustshine_windows() { _build_rustshine_binary "$1"; }
fetch_rustshine_macos() { _build_rustshine_binary "$1"; }
