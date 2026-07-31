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

# _build_rustshine_binary <dest_dir> [cargo feature args...]
# Builds bin/gamestream-server in release mode from RUSTSHINE_SRC_DIR and
# copies the resulting binary into dest_dir. Common to all three OSes: on
# Linux/macOS this is a native `cargo build`; on Windows this script is
# invoked from inside MSYS2 UCRT64, which is also a native (not
# cross-compiled) target for a Rust toolchain installed there, so the same
# plain `cargo build --release` applies. Extra cargo args (feature flags)
# are the caller's job, not this function's -- see each `fetch_rustshine_*`
# wrapper below for which one it needs and why: `gamestream-server`'s own
# `desktop` feature used to bundle two independent things (Linux-only
# KMS/DRM+VAAPI capture, and skipping the SBC/Mender-bound license gate
# that doesn't make sense on any desktop OS) into one flag, which broke a
# Windows build two different ways in a row -- first a hard compile failure
# (`dep:capture-kms` isn't target-gated, so enabling it tries to compile
# real DRM ioctl code on Windows: `cannot find crate libc`), then, after
# dropping the feature entirely, a silent behavior regression (without
# `license/desktop`, `license::check()` runs the SBC-oriented Mender-key/
# devicetree-serial check instead of always-`Licensed`, neither of which
# exists on a Windows desktop). rust-shine's own Cargo.toml now splits this
# into `desktop-linux-capture` and `desktop-license` so each platform can
# ask for exactly what it needs.
_build_rustshine_binary() {
    local dest_dir="$1"
    shift
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
        # rustup's default install (`.cargo/bin` under the user's home) is
        # never on PATH inside a fresh MSYS2 shell -- MSYS2 doesn't inherit
        # the Windows user's PATH modifications the rustup-init installer
        # makes, only whatever pacman itself adds (Go, gcc, etc., which is
        # why build_windows.sh's own `add_to_path_if_exists` calls found
        # those fine but this needs its own check). Try the two places a
        # real rustup install can live before giving up: `$USERPROFILE`
        # (the actual Windows env var, inherited into MSYS2 regardless of
        # how MSYS2's own $HOME is mapped) and MSYS2's own $HOME, in case
        # it's been pointed at the Windows profile directory.
        for _cargo_candidate_home in \
            "${USERPROFILE:+$(cygpath -u "$USERPROFILE" 2>/dev/null)}" \
            "$HOME"
        do
            [[ -n "$_cargo_candidate_home" && -x "$_cargo_candidate_home/.cargo/bin/cargo.exe" ]] || continue
            export PATH="$_cargo_candidate_home/.cargo/bin:$PATH"
            break
        done
    fi

    if ! command -v cargo >/dev/null 2>&1; then
        echo -e "${RED}cargo not found in PATH — cannot build rust-shine${NC}" >&2
        echo "Install Rust (https://rustup.rs) if you haven't, or if it's already" >&2
        echo "installed, make sure \$USERPROFILE/.cargo/bin exists and is a real" >&2
        echo "rustup install (this looks in \$USERPROFILE and \$HOME, in that order)." >&2
        exit 1
    fi

    echo -e "${YELLOW}Building gamestream-server (release) from $src_dir...${NC}"
    ( cd "$src_dir" && cargo build --release "$@" -p gamestream-server )

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

fetch_rustshine_linux() { _build_rustshine_binary "$1" --features desktop; }
# `desktop-license` only, not the full `desktop` -- see `_build_rustshine_binary`'s
# own docs: neither Windows nor macOS wants (or can compile)
# `desktop-linux-capture`'s KMS/DRM+VAAPI half, but both still need the
# license gate skipped the same way Linux desktop does.
fetch_rustshine_windows() { _build_rustshine_binary "$1" --features desktop-license; }
fetch_rustshine_macos() { _build_rustshine_binary "$1" --features desktop-license; }
