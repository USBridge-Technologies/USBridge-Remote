#!/usr/bin/env bash
# Packs the client Flatpak from the AppDir that client/scripts/build_linux.sh
# assembles (the one the AppImage, .deb, Arch package, RPM and snap come from),
# so nothing is compiled twice and FFmpeg, ONNX Runtime and the rest are the
# same files. That is why there is no flatpak-builder manifest here: it's a
# build for our own repository (publish_repo.sh). Flathub only takes builds
# from source, and it doesn't take manifests written with AI tools, so this
# script must not be turned into a Flathub submission.
#
#   build_bundle.sh APPDIR VERSION OUT.flatpak
#
# Needs flatpak and org.freedesktop.Platform//$RUNTIME_VERSION installed.
#
# The Platform stands in for the SDK in build-init because nothing is built
# in the sandbox. Sandbox: the same reach as the snap's plugs, where
# --device=all stands for joystick, camera, raw-usb, alsa and the GPU; a Fyne
# build without the wayland tag is an X11 client, so --socket=x11 and not
# fallback-x11, which drops X on a Wayland session. pkexec isn't in the
# sandbox, so the Wi-Fi tune uses NetworkManager (--system-talk-name) and the
# updater stays off (internal/netutil, internal/update, $FLATPAK_ID).
set -euo pipefail

APP_ID=io.usbridge.Client
RUNTIME_VERSION=26.08

APPDIR="$(realpath "$1")"
VERSION="$2"
OUT="$(realpath -m "$3")"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
build="$work/build"

flatpak build-init "$build" "$APP_ID" org.freedesktop.Platform org.freedesktop.Platform "$RUNTIME_VERSION"

cp -a "$APPDIR/usr/." "$build/files/"
# An AppDir unpacked with --appimage-extract keeps the squashfs root's 0700.
chmod -R u+rwX,go+rX,go-w "$build/files"
# Exported launcher and icon have to carry the app ID.
rm -rf "$build/files/share/applications" "$build/files/share/icons" "$build/files/share/pixmaps"
install -Dm644 "$SCRIPT_DIR/$APP_ID.desktop" "$build/files/share/applications/$APP_ID.desktop"
install -Dm644 "$REPO_ROOT/client/Icon.png" "$build/files/share/icons/hicolor/512x512/apps/$APP_ID.png"

flatpak build-finish "$build" \
    --command=usbridge-client \
    --share=network \
    --share=ipc \
    --socket=x11 \
    --socket=pulseaudio \
    --device=all \
    --filesystem=home \
    --filesystem=/run/media \
    --filesystem=/media \
    --filesystem=/run/udev:ro \
    --system-talk-name=org.freedesktop.NetworkManager

flatpak build-export --subject="USBridge Client $VERSION" "$work/repo" "$build" stable
mkdir -p "$(dirname "$OUT")"
flatpak build-bundle --runtime-repo=https://dl.flathub.org/repo/flathub.flatpakrepo \
    "$work/repo" "$OUT" "$APP_ID" stable
ls -la "$OUT"
