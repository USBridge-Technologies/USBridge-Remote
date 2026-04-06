#!/bin/bash

# Общие функции поиска Android SDK/NDK для build-скриптов.

add_to_path_if_exists() {
    local dir="$1"

    if [ -z "$dir" ] || [ ! -d "$dir" ]; then
        return 0
    fi

    case ":$PATH:" in
        *":$dir:"*) ;;
        *) export PATH="$dir:$PATH" ;;
    esac

    return 0
}

is_windows_env() {
    case "$(uname -s 2>/dev/null || echo unknown)" in
        MINGW*|MSYS*|CYGWIN*) return 0 ;;
    esac

    [ -n "${WINDIR:-}" ] || [ -n "${LOCALAPPDATA:-}" ]
}

is_macos_env() {
    [ "$(uname -s 2>/dev/null || echo unknown)" = "Darwin" ]
}

is_linux_env() {
    [ "$(uname -s 2>/dev/null || echo unknown)" = "Linux" ]
}

run_powershell() {
    if command -v powershell.exe >/dev/null 2>&1; then
        powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "$@"
        return $?
    fi

    if command -v pwsh >/dev/null 2>&1; then
        pwsh -NoProfile -Command "$@"
        return $?
    fi

    return 1
}

resolve_windows_jdk_home() {
    local candidate=""

    for candidate in \
        "/c/Program Files/Microsoft/jdk-21.0.10.7-hotspot" \
        "/c/Program Files/Microsoft/jdk-21" \
        "/c/Program Files/Android/Android Studio/jbr" \
        "/c/Program Files/Java/jdk-21" \
        "/c/Program Files/Java/jdk-21.0.10" \
        "/c/Program Files/Java/jdk-17" \
        "/c/Program Files/Java/jdk-11"
    do
        if [ -d "$candidate/bin" ]; then
            printf '%s\n' "$candidate"
            return 0
        fi
    done

    candidate="$(run_powershell "& {
        \$paths = @()
        \$regPaths = @(
            'HKLM:\\SOFTWARE\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\*',
            'HKLM:\\SOFTWARE\\WOW6432Node\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\*'
        )
        foreach (\$regPath in \$regPaths) {
            Get-ItemProperty \$regPath -ErrorAction SilentlyContinue |
                Where-Object { \$_.DisplayName -like '*OpenJDK*' -or \$_.DisplayName -like '*JDK*' } |
                ForEach-Object {
                    if (\$_.InstallLocation) { \$paths += \$_.InstallLocation }
                }
        }
        \$paths | Select-Object -First 1
    }" | tr -d '\r' | tail -n 1)"
    if [ -n "$candidate" ]; then
        candidate="$(normalize_android_path "$candidate" 2>/dev/null || true)"
        if [ -n "$candidate" ] && [ -d "$candidate/bin" ]; then
            printf '%s\n' "$candidate"
            return 0
        fi
    fi

    return 1
}

refresh_bootstrap_paths() {
    local jdk_home=""

    add_to_path_if_exists "$(msys2_current_bin_dir 2>/dev/null || true)"
    add_to_path_if_exists "$HOME/.local/bin"
    add_to_path_if_exists "$HOME/bin"
    add_to_path_if_exists "/usr/local/bin"
    add_to_path_if_exists "/opt/homebrew/bin"
    add_to_path_if_exists "/opt/homebrew/opt/flex/bin"
    add_to_path_if_exists "/opt/homebrew/opt/bison/bin"
    add_to_path_if_exists "/opt/homebrew/opt/openjdk@21/bin"
    add_to_path_if_exists "/usr/local/opt/flex/bin"
    add_to_path_if_exists "/usr/local/opt/bison/bin"
    add_to_path_if_exists "/usr/local/opt/openjdk@21/bin"
    add_to_path_if_exists "/mingw64/bin"
    add_to_path_if_exists "/ucrt64/bin"
    add_to_path_if_exists "/clang64/bin"

    if [ -n "${LOCALAPPDATA:-}" ]; then
        add_to_path_if_exists "$(normalize_android_path "$LOCALAPPDATA/Microsoft/WindowsApps" 2>/dev/null || true)"
        add_to_path_if_exists "$(normalize_android_path "$LOCALAPPDATA/Microsoft/WinGet/Links" 2>/dev/null || true)"

        local py_scripts=""
        for py_scripts in "$LOCALAPPDATA"/Programs/Python/Python*/Scripts; do
            [ -d "$py_scripts" ] && add_to_path_if_exists "$(normalize_android_path "$py_scripts" 2>/dev/null || true)"
        done
        for py_scripts in "$LOCALAPPDATA"/Programs/Python/Python*; do
            [ -d "$py_scripts" ] && add_to_path_if_exists "$(normalize_android_path "$py_scripts" 2>/dev/null || true)"
        done
    fi

    add_to_path_if_exists "/c/Program Files/Git/cmd"
    add_to_path_if_exists "/c/Program Files/Git/bin"
    add_to_path_if_exists "/c/ProgramData/chocolatey/bin"

    jdk_home="$(resolve_windows_jdk_home 2>/dev/null || true)"
    if [ -n "$jdk_home" ]; then
        export JAVA_HOME="$jdk_home"
        add_to_path_if_exists "$jdk_home/bin"
    fi

    hash -r 2>/dev/null || true
}

add_android_sdk_tools_to_path() {
    local sdk_dir="$1"

    [ -n "$sdk_dir" ] || return 0

    add_to_path_if_exists "$sdk_dir/platform-tools"
    add_to_path_if_exists "$sdk_dir/emulator"
    add_to_path_if_exists "$sdk_dir/cmdline-tools/latest/bin"
    add_to_path_if_exists "$sdk_dir/cmdline-tools/bin"

    local tool_dir=""
    for tool_dir in "$sdk_dir"/cmdline-tools/*/bin; do
        [ -d "$tool_dir" ] && add_to_path_if_exists "$tool_dir"
    done
}

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

msys2_mingw_prefix() {
    case "${MSYSTEM:-}" in
        UCRT64) echo "mingw-w64-ucrt-x86_64" ;;
        CLANG64) echo "mingw-w64-clang-x86_64" ;;
        CLANGARM64) echo "mingw-w64-clang-aarch64" ;;
        MINGW64) echo "mingw-w64-x86_64" ;;
        MINGW32) echo "mingw-w64-i686" ;;
        *) return 1 ;;
    esac
}

msys2_current_bin_dir() {
    case "${MSYSTEM:-}" in
        UCRT64) echo "/ucrt64/bin" ;;
        CLANG64) echo "/clang64/bin" ;;
        CLANGARM64) echo "/clangarm64/bin" ;;
        MINGW64) echo "/mingw64/bin" ;;
        MINGW32) echo "/mingw32/bin" ;;
        *) return 1 ;;
    esac
}

command_path() {
    command -v "$1" 2>/dev/null | tr -d '\r' | tail -n 1
}

resolve_command_path() {
    local tool="$1"
    local candidate=""
    local expected_bin=""

    expected_bin="$(msys2_current_bin_dir 2>/dev/null || true)"

    if [ -n "$expected_bin" ]; then
        case "$tool" in
            python3)
                for candidate in "$expected_bin/python3.exe" "$expected_bin/python.exe"; do
                    if [ -x "$candidate" ]; then
                        echo "$candidate"
                        return 0
                    fi
                done
                ;;
            java|keytool)
                local jdk_home=""
                jdk_home="$(resolve_windows_jdk_home 2>/dev/null || true)"
                if [ -n "$jdk_home" ]; then
                    for candidate in "$jdk_home/bin/$tool.exe" "$jdk_home/bin/$tool"; do
                        if [ -x "$candidate" ]; then
                            echo "$candidate"
                            return 0
                        fi
                    done
                fi
                ;;
            meson|ninja|python|git|flex|bison|unzip|java|keytool|go)
                for candidate in "$expected_bin/$tool.exe" "$expected_bin/$tool"; do
                    if [ -x "$candidate" ]; then
                        echo "$candidate"
                        return 0
                    fi
                done
                ;;
        esac
    fi

    case "$tool" in
        python3)
            for candidate in python3 python; do
                if command -v "$candidate" >/dev/null 2>&1; then
                    command_path "$candidate"
                    return 0
                fi
            done
            return 1
            ;;
        *)
            command_path "$tool"
            ;;
    esac
}

is_msys2_mingw_tool_compatible() {
    local tool="$1"
    local tool_path=""
    local expected_bin=""

    case "$tool" in
        flex|bison|git|unzip|java|keytool) return 0 ;;
    esac

    expected_bin="$(msys2_current_bin_dir 2>/dev/null || true)"
    if [ -z "$expected_bin" ]; then
        return 0
    fi

    tool_path="$(resolve_command_path "$tool")"
    if [ -z "$tool_path" ]; then
        return 1
    fi

    case "$tool_path" in
        "$expected_bin"/*) return 0 ;;
    esac

    return 1
}

can_use_sudo() {
    command -v sudo >/dev/null 2>&1
}

install_packages_apt() {
    if ! command -v apt-get >/dev/null 2>&1; then
        return 1
    fi

    if can_use_sudo; then
        sudo apt-get update && sudo apt-get install -y "$@"
    else
        apt-get update && apt-get install -y "$@"
    fi
}

install_packages_pacman() {
    if ! command -v pacman >/dev/null 2>&1; then
        return 1
    fi

    pacman -S --needed --noconfirm "$@"
}

install_packages_brew() {
    if ! command -v brew >/dev/null 2>&1; then
        return 1
    fi

    brew install "$@"
}

install_windows_package_winget() {
    local package_id="$1"

    if [ -z "$package_id" ]; then
        return 1
    fi

    run_powershell "& {
        \$ErrorActionPreference = 'Stop'
        if (-not (Get-Command winget -ErrorAction SilentlyContinue)) { exit 1 }
        winget install --exact --id '$package_id' --accept-package-agreements --accept-source-agreements --silent
    }"
}

ensure_winflex_bison_wrappers() {
    if command -v flex >/dev/null 2>&1 && command -v bison >/dev/null 2>&1; then
        return 0
    fi

    local win_flex_path=""
    local win_bison_path=""
    local wrapper_dir="$HOME/.usbridge-tools/bin"

    win_flex_path="$(run_powershell "& {
        \$cmd = Get-Command win_flex -ErrorAction SilentlyContinue
        if (\$cmd) { \$cmd.Source }
    }" | tr -d '\r' | tail -n 1)"
    win_bison_path="$(run_powershell "& {
        \$cmd = Get-Command win_bison -ErrorAction SilentlyContinue
        if (\$cmd) { \$cmd.Source }
    }" | tr -d '\r' | tail -n 1)"

    if [ -z "$win_flex_path" ] || [ -z "$win_bison_path" ]; then
        return 1
    fi

    mkdir -p "$wrapper_dir"

    cat > "$wrapper_dir/flex" << EOF
#!/bin/bash
"$win_flex_path" "\$@"
EOF
    cat > "$wrapper_dir/bison" << EOF
#!/bin/bash
"$win_bison_path" "\$@"
EOF
    chmod +x "$wrapper_dir/flex" "$wrapper_dir/bison"
    add_to_path_if_exists "$wrapper_dir"
    hash -r 2>/dev/null || true

    command -v flex >/dev/null 2>&1 && command -v bison >/dev/null 2>&1
}

install_python_build_tools() {
    local python_cmd=""

    for python_cmd in python3 python py; do
        if command -v "$python_cmd" >/dev/null 2>&1; then
            "$python_cmd" -m pip install --user meson ninja
            refresh_bootstrap_paths
            command -v meson >/dev/null 2>&1 && command -v ninja >/dev/null 2>&1 && return 0
        fi
    done

    return 1
}

install_tool_package() {
    local tool="$1"
    local msys_prefix=""

    msys_prefix="$(msys2_mingw_prefix 2>/dev/null || true)"

    case "$tool" in
        python3|python)
            if [ -n "$msys_prefix" ] && install_packages_pacman "${msys_prefix}-python"; then return 0; fi
            if is_msys2_env && install_packages_pacman python; then return 0; fi
            if is_linux_env && install_packages_apt python3 python3-pip; then return 0; fi
            if is_macos_env && install_packages_brew python; then return 0; fi
            if is_windows_env && install_windows_package_winget Python.Python.3.12; then return 0; fi
            ;;
        git)
            if is_msys2_env && install_packages_pacman git; then return 0; fi
            if is_linux_env && install_packages_apt git; then return 0; fi
            if is_macos_env && install_packages_brew git; then return 0; fi
            if is_windows_env && install_windows_package_winget Git.Git; then return 0; fi
            ;;
        flex|bison)
            if [ -n "$msys_prefix" ] && install_packages_pacman "${msys_prefix}-winflexbison"; then return 0; fi
            if is_msys2_env && install_packages_pacman flex bison; then return 0; fi
            if is_linux_env && install_packages_apt flex bison; then return 0; fi
            if is_macos_env && install_packages_brew flex bison; then return 0; fi
            if is_windows_env && install_windows_package_winget lexxmark.winflexbison; then
                refresh_bootstrap_paths
                ensure_winflex_bison_wrappers && return 0
            fi
            ;;
        meson|ninja)
            if [ -n "$msys_prefix" ] && install_packages_pacman "${msys_prefix}-python" "${msys_prefix}-meson" "${msys_prefix}-ninja"; then return 0; fi
            if is_msys2_env && install_packages_pacman meson ninja; then return 0; fi
            if is_linux_env && install_packages_apt meson ninja-build; then return 0; fi
            if is_macos_env && install_packages_brew meson ninja; then return 0; fi
            install_python_build_tools && return 0
            ;;
        nasm)
            if [ -n "$msys_prefix" ] && install_packages_pacman "${msys_prefix}-nasm"; then return 0; fi
            if is_msys2_env && install_packages_pacman nasm; then return 0; fi
            if is_linux_env && install_packages_apt nasm; then return 0; fi
            if is_macos_env && install_packages_brew nasm; then return 0; fi
            if is_windows_env && install_windows_package_winget NASM.NASM; then return 0; fi
            ;;
        unzip)
            if [ -n "$msys_prefix" ] && install_packages_pacman "${msys_prefix}-unzip"; then return 0; fi
            if is_msys2_env && install_packages_pacman unzip; then return 0; fi
            if is_linux_env && install_packages_apt unzip; then return 0; fi
            if is_macos_env && install_packages_brew unzip; then return 0; fi
            ;;
        java|keytool)
            if [ -n "$msys_prefix" ] && install_packages_pacman "${msys_prefix}-jdk-openjdk"; then return 0; fi
            if is_msys2_env && install_packages_pacman jdk-openjdk; then return 0; fi
            if is_linux_env && install_packages_apt openjdk-21-jdk; then return 0; fi
            if is_macos_env && install_packages_brew openjdk@21; then return 0; fi
            if is_windows_env && install_windows_package_winget Microsoft.OpenJDK.21; then return 0; fi
            ;;
        go)
            if [ -n "$msys_prefix" ] && install_packages_pacman "${msys_prefix}-go"; then return 0; fi
            if is_msys2_env && install_packages_pacman go; then return 0; fi
            if is_linux_env && install_packages_apt golang-go; then return 0; fi
            if is_macos_env && install_packages_brew go; then return 0; fi
            if is_windows_env && install_windows_package_winget GoLang.Go; then return 0; fi
            ;;
        *)
            return 1
            ;;
    esac

    return 1
}

ensure_command_available() {
    local tool="$1"
    local friendly_name="${2:-$1}"
    local need_install=0

    refresh_bootstrap_paths
    if [ -n "$(resolve_command_path "$tool")" ]; then
        if ! is_msys2_env || is_msys2_mingw_tool_compatible "$tool"; then
            return 0
        fi
        need_install=1
    fi

    if [ "$need_install" -eq 1 ]; then
        echo "📥 Найден несовместимый $friendly_name вне текущего MSYS2 toolchain. Переключаю на пакет для ${MSYSTEM:-MSYS2}..."
    else
        echo "📥 Не найден $friendly_name. Пытаюсь установить автоматически..."
    fi
    if install_tool_package "$tool"; then
        refresh_bootstrap_paths
        if [ "$tool" = "flex" ] || [ "$tool" = "bison" ]; then
            ensure_winflex_bison_wrappers >/dev/null 2>&1 || true
        fi
    fi

    refresh_bootstrap_paths
    if [ -n "$(resolve_command_path "$tool")" ] && { ! is_msys2_env || is_msys2_mingw_tool_compatible "$tool"; }; then
        echo "✓ $friendly_name установлен"
        return 0
    fi

    return 1
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
        add_android_sdk_tools_to_path "$sdk_dir"
    fi

    if [ -n "$ndk_dir" ]; then
        export ANDROID_NDK_HOME="$ndk_dir"
        export ANDROID_NDK_ROOT="$ndk_dir"
    fi
}

resolve_android_sdkmanager() {
    local sdk_dir=""
    local candidate=""

    sdk_dir="$(resolve_android_sdk 2>/dev/null || true)"
    for candidate in \
        "${ANDROID_HOME:-}/cmdline-tools/latest/bin/sdkmanager" \
        "${ANDROID_HOME:-}/cmdline-tools/bin/sdkmanager" \
        "${ANDROID_SDK_ROOT:-}/cmdline-tools/latest/bin/sdkmanager" \
        "${ANDROID_SDK_ROOT:-}/cmdline-tools/bin/sdkmanager"
    do
        if [ -x "$candidate" ]; then
            printf '%s\n' "$candidate"
            return 0
        fi
    done

    if [ -n "$sdk_dir" ]; then
        for candidate in "$sdk_dir"/cmdline-tools/*/bin/sdkmanager; do
            if [ -x "$candidate" ]; then
                printf '%s\n' "$candidate"
                return 0
            fi
        done
    fi

    command_path sdkmanager
}

ensure_android_sdk_package() {
    local package_name="$1"
    local package_dir="$2"
    local sdk_dir=""
    local sdkmanager_cmd=""

    export_android_env
    sdk_dir="$(resolve_android_sdk 2>/dev/null || true)"
    if [ -z "$sdk_dir" ] || [ ! -d "$sdk_dir" ]; then
        return 1
    fi

    if [ -n "$package_dir" ] && [ -d "$sdk_dir/$package_dir" ]; then
        return 0
    fi

    sdkmanager_cmd="$(resolve_android_sdkmanager 2>/dev/null || true)"
    if [ -z "$sdkmanager_cmd" ] || [ ! -x "$sdkmanager_cmd" ]; then
        return 1
    fi

    echo "📥 Не найден Android SDK package $package_name. Устанавливаю автоматически..."
    if ! yes | "$sdkmanager_cmd" --sdk_root="$sdk_dir" "$package_name" >/dev/null; then
        return 1
    fi

    [ -z "$package_dir" ] || [ -d "$sdk_dir/$package_dir" ]
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
