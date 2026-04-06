#!/bin/bash

# Общие функции поиска Android SDK/NDK для build-скриптов.

normalize_android_path() {
    local path_value="$1"

    if [ -z "$path_value" ]; then
        return 1
    fi

    if [ -d "$path_value" ]; then
        echo "$path_value"
        return 0
    fi

    if command -v cygpath >/dev/null 2>&1; then
        local converted=""
        converted="$(cygpath -u "$path_value" 2>/dev/null || true)"
        if [ -n "$converted" ] && [ -d "$converted" ]; then
            echo "$converted"
            return 0
        fi
    fi

    return 1
}

is_msys2_env() {
    [ -n "${MSYSTEM:-}" ] || [ -n "${MSYS:-}" ] || [ -x "/usr/bin/pacman" ] || [ -x "/mingw64/bin/pacman" ]
}

print_flex_install_hint() {
    if is_msys2_env; then
        echo "   Установите flex в MSYS2: pacman -S --needed flex bison"
        echo "   Если команда не найдена, запустите её в оболочке MSYS2/UCRT64"
        return 0
    fi

    echo "   Установите flex (например: sudo apt-get install flex)"
}

find_latest_ndk_in_dir() {
    local base_dir="$1"

    if [ -z "$base_dir" ] || [ ! -d "$base_dir" ]; then
        return 1
    fi

    find "$base_dir" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | sort -V | tail -n 1
}

resolve_android_sdk() {
    local candidate=""
    local normalized=""

    for candidate in \
        "${ANDROID_HOME:-}" \
        "${ANDROID_SDK_ROOT:-}" \
        "${LOCALAPPDATA:-}/Android/Sdk" \
        "${LOCALAPPDATA:-}/Android/sdk" \
        "$HOME/Android/Sdk" \
        "$HOME/Android/sdk" \
        "$HOME/Library/Android/sdk" \
        "/usr/lib/android-sdk"
    do
        normalized="$(normalize_android_path "$candidate" 2>/dev/null || true)"
        if [ -n "$normalized" ]; then
            echo "$normalized"
            return 0
        fi
    done

    return 1
}

resolve_android_ndk() {
    local candidate=""
    local normalized=""
    local sdk_dir=""
    local ndk_dir=""

    for candidate in \
        "${ANDROID_NDK_HOME:-}" \
        "${ANDROID_NDK_ROOT:-}"
    do
        normalized="$(normalize_android_path "$candidate" 2>/dev/null || true)"
        if [ -n "$normalized" ]; then
            echo "$normalized"
            return 0
        fi
    done

    sdk_dir="$(resolve_android_sdk 2>/dev/null || true)"
    if [ -n "$sdk_dir" ]; then
        for ndk_dir in "$sdk_dir/ndk" "$sdk_dir/ndk-bundle"; do
            if [ -d "$ndk_dir" ]; then
                if [ "$(basename "$ndk_dir")" = "ndk-bundle" ]; then
                    echo "$ndk_dir"
                    return 0
                fi

                candidate="$(find_latest_ndk_in_dir "$ndk_dir" 2>/dev/null || true)"
                if [ -n "$candidate" ] && [ -d "$candidate" ]; then
                    echo "$candidate"
                    return 0
                fi
            fi
        done
    fi

    if [ -d "/usr/lib/android-ndk" ]; then
        echo "/usr/lib/android-ndk"
        return 0
    fi

    return 1
}

export_android_env() {
    local sdk_dir=""
    local ndk_dir=""

    sdk_dir="$(resolve_android_sdk 2>/dev/null || true)"
    ndk_dir="$(resolve_android_ndk 2>/dev/null || true)"

    if [ -n "$sdk_dir" ]; then
        export ANDROID_HOME="$sdk_dir"
        export ANDROID_SDK_ROOT="$sdk_dir"
    fi

    if [ -n "$ndk_dir" ]; then
        export ANDROID_NDK_HOME="$ndk_dir"
        export ANDROID_NDK_ROOT="$ndk_dir"
    fi
}

setup_android_ndk_toolchain_env() {
    local ndk_dir="$1"
    local api_level="${2:-28}"
    local prebuilt_dir=""

    if [ -z "$ndk_dir" ] || [ ! -d "$ndk_dir" ]; then
        return 1
    fi

    prebuilt_dir="$(find "$ndk_dir/toolchains/llvm/prebuilt" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | sort | head -n 1)"
    if [ -z "$prebuilt_dir" ] || [ ! -d "$prebuilt_dir" ]; then
        return 1
    fi

    export AR="$prebuilt_dir/bin/llvm-ar"
    export AS="$prebuilt_dir/bin/llvm-as"
    export CC="$prebuilt_dir/bin/aarch64-linux-android${api_level}-clang"
    export CXX="$prebuilt_dir/bin/aarch64-linux-android${api_level}-clang++"
    export LD="$prebuilt_dir/bin/ld.lld"
    export NM="$prebuilt_dir/bin/llvm-nm"
    export OBJCOPY="$prebuilt_dir/bin/llvm-objcopy"
    export RANLIB="$prebuilt_dir/bin/llvm-ranlib"
    export STRIP="$prebuilt_dir/bin/llvm-strip"
}

meson_builddir_needs_reset() {
    local build_dir="$1"

    if [ ! -d "$build_dir" ]; then
        return 1
    fi

    if rg -q "/opt/homebrew|darwin-x86_64|/Users/" "$build_dir/meson-private" "$build_dir/meson-info" "$build_dir/meson-logs" 2>/dev/null; then
        return 0
    fi

    return 1
}
