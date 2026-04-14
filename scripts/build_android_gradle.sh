#!/bin/bash
# Build USBridge Client for Android with Gradle
# Includes MainActivity with camera, QR scanner, and SAF

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

SCRIPTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPTS_DIR/.." && pwd)"
source "$SCRIPTS_DIR/android_env.sh"
refresh_bootstrap_paths
cd "$REPO_ROOT"

if [ -z "${USBRIDGE_LOGGING_ACTIVE:-}" ]; then
    export USBRIDGE_LOGGING_ACTIVE=1
    LOG_DIR="$REPO_ROOT/logs"
    mkdir -p "$LOG_DIR"
    LOG_FILE="$LOG_DIR/$(basename "$0" .sh).log"
    exec > >(tee -a "$LOG_FILE") 2>&1
    echo "=== $(date '+%Y-%m-%d %H:%M:%S') [$0] ==="
fi

DIST_DIR="$REPO_ROOT/dist/android"
mkdir -p "$DIST_DIR"

echo "=============================================="
echo "  USBridge Client - Gradle build (camera)"
echo "=============================================="
echo ""

# 1. GStreamer — динамическая линковка (.so)
# Для Android используем только dynamic build: CGO в проекте смотрит на
# gstreamer-android-dynamic/{include,lib}. Старый prebuilt fallback тянет
# другой набор библиотек и на чистых машинах даёт невоспроизводимую сборку.
echo "📦 Шаг 1/5: GStreamer (dynamic .so)..."
"$SCRIPTS_DIR/build_gstreamer_dynamic_android.sh"
echo ""

# 2. gomobile bind для nbdbridge
echo "📦 Шаг 2/5: gomobile bind nbdbridge..."

# 2.5 Tailscale CLI & Daemon for Android
echo "📦 Шаг 2.5/5: Building Tailscale binaries..."
mkdir -p android/app/src/main/jniLibs/arm64-v8a

# Configure NDK toolchain for CGO cross-compilation
# We use API level 24 (matching gomobile bind)
(
    export_android_env
    if [ -n "${ANDROID_NDK_HOME:-}" ] && [ -d "$ANDROID_NDK_HOME" ]; then
        setup_android_ndk_toolchain_env "$ANDROID_NDK_HOME" 24
        echo "   Using NDK toolchain: $CC"
    else
        echo -e "${RED}❌ ANDROID_NDK_HOME not set, CGO build may fail${NC}"
    fi

    export GOOS=android
    export GOARCH=arm64
    export CGO_ENABLED=1
    
    # We use multiple tags to ensure GUI and desktop dependencies are skipped.
    # nosystray: skip fyne.io/systray which is not supported on Android.
    # omitgui, ts_omit_gui: Tailscale-specific tags to skip GUI/systray.
    BUILD_TAGS="omitgui,ts_omit_gui,nosystray,ts_omit_systray"
    
    go build -v -trimpath -tags="$BUILD_TAGS" -ldflags="-s -w" -o android/app/src/main/jniLibs/arm64-v8a/libtailscale.so tailscale.com/cmd/tailscale
    go build -v -trimpath -tags="$BUILD_TAGS" -ldflags="-s -w" -o android/app/src/main/jniLibs/arm64-v8a/libtailscaled.so tailscale.com/cmd/tailscaled
)
echo -e "${GREEN}✓${NC} Tailscale binaries (libtailscale.so, libtailscaled.so) built"
if ! ensure_command_available go Go; then
    echo -e "${RED}❌ Go не найден${NC}"
    exit 1
fi
GOBIN="$(go env GOBIN 2>/dev/null)"
GOPATH_BIN="$(go env GOPATH 2>/dev/null)/bin"

find_go_tool() {
    local tool="$1"

    if [ -n "$GOBIN" ] && [ -x "$GOBIN/$tool" ]; then
        echo "$GOBIN/$tool"
        return 0
    fi
    if [ -x "$GOPATH_BIN/$tool" ]; then
        echo "$GOPATH_BIN/$tool"
        return 0
    fi
    if command -v "$tool" >/dev/null 2>&1; then
        command -v "$tool"
        return 0
    fi

    return 1
}

path_is_newer_than() {
    local target="$1"
    shift

    [ -f "$target" ] || return 0

    local candidate=""
    for candidate in "$@"; do
        [ -e "$candidate" ] || continue
        if [ "$candidate" -nt "$target" ]; then
            return 0
        fi
    done

    return 1
}

tree_has_newer_files() {
    local target="$1"
    shift

    [ -f "$target" ] || return 0

    local tree=""
    for tree in "$@"; do
        [ -e "$tree" ] || continue
        if find "$tree" -type f -newer "$target" | grep -q .; then
            return 0
        fi
    done

    return 1
}

sync_file_if_needed() {
    local src="$1"
    local dst="$2"

    [ -f "$src" ] || return 0
    mkdir -p "$(dirname "$dst")"

    if [ -f "$dst" ] && files_are_identical "$src" "$dst"; then
        return 0
    fi

    cp -f "$src" "$dst"
}

file_sha256() {
    local path="$1"

    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$path" | awk '{print $1}'
        return 0
    fi
    if command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$path" | awk '{print $1}'
        return 0
    fi
    if command -v openssl >/dev/null 2>&1; then
        openssl dgst -sha256 "$path" | awk '{print $NF}'
        return 0
    fi
    if command -v powershell >/dev/null 2>&1; then
        powershell -NoProfile -Command "(Get-FileHash -Algorithm SHA256 -LiteralPath '$path').Hash.ToLowerInvariant()"
        return 0
    fi

    return 1
}

files_are_identical() {
    local src="$1"
    local dst="$2"
    local src_hash=""
    local dst_hash=""

    if command -v cmp >/dev/null 2>&1; then
        cmp -s "$src" "$dst"
        return $?
    fi

    src_hash="$(file_sha256 "$src" 2>/dev/null || true)"
    dst_hash="$(file_sha256 "$dst" 2>/dev/null || true)"
    if [ -n "$src_hash" ] && [ -n "$dst_hash" ]; then
        [ "$src_hash" = "$dst_hash" ]
        return $?
    fi

    return 1
}

aar_looks_valid() {
    local aar_path="$1"

    [ -f "$aar_path" ] || return 1
    [ -s "$aar_path" ] || return 1

    if command -v unzip >/dev/null 2>&1; then
        unzip -tqq "$aar_path" >/dev/null 2>&1 || return 1
    fi

    return 0
}

ensure_go_tool() {
    local tool="$1"
    local pkg="$2"
    local force_install="${3:-0}"
    local tool_path=""

    if [ "$force_install" != "1" ]; then
        tool_path="$(find_go_tool "$tool" 2>/dev/null || true)"
    fi
    if [ "$force_install" != "1" ] && [ -n "$tool_path" ]; then
        echo "$tool_path"
        return 0
    fi

    echo -e "${YELLOW}Установка $tool...${NC}" >&2
    if ! go install "$pkg"; then
        echo -e "${RED}❌ Не удалось установить $tool${NC}" >&2
        echo "   Проверьте доступ к сети и команду: go install $pkg" >&2
        exit 1
    fi

    tool_path="$(find_go_tool "$tool" 2>/dev/null || true)"
    if [ -z "$tool_path" ]; then
        echo -e "${RED}❌ $tool не найден после установки${NC}" >&2
        echo "   Проверьте PATH или используйте: $(go env GOPATH)/bin/$tool" >&2
        exit 1
    fi

    echo "$tool_path"
}

GOMOBILE_CMD="$(ensure_go_tool gomobile golang.org/x/mobile/cmd/gomobile@latest)"
GOBIND_CMD="$(find_go_tool gobind 2>/dev/null || true)"

if [ -n "$GOBIN" ] && [ -d "$GOBIN" ]; then
    export PATH="$GOBIN:$PATH"
fi
if [ -d "$GOPATH_BIN" ] && [[ ":$PATH:" != *":$GOPATH_BIN:"* ]]; then
    export PATH="$GOPATH_BIN:$PATH"
fi

if ! ensure_command_available java Java; then
    echo -e "${RED}❌ Java не найден${NC}"
    exit 1
fi

# Убедимся, что ANDROID_HOME/ANDROID_NDK_HOME заданы до gomobile init
export_android_env
if ! ensure_android_sdk_package "platforms;android-24" "platforms/android-24"; then
    echo -e "${RED}❌ Android SDK platform android-24 не найден${NC}"
    echo "   Установите через sdkmanager: platforms;android-24"
    exit 1
fi

"$GOMOBILE_CMD" init || {
    echo -e "${RED}❌ gomobile init не удался${NC}"
    echo "   Проверьте ANDROID_HOME/ANDROID_NDK_HOME и установленный JDK"
    exit 1
}

GOBIND_CMD="$(find_go_tool gobind 2>/dev/null || true)"
if [ -z "$GOBIND_CMD" ]; then
    GOBIND_CMD="$(ensure_go_tool gobind golang.org/x/mobile/cmd/gobind@latest)"
fi

if [ -z "$GOBIND_CMD" ]; then
    echo -e "${RED}❌ gobind не найден после gomobile init${NC}"
    echo "   Попробуйте: go install golang.org/x/mobile/cmd/gobind@latest"
    exit 1
fi

mkdir -p android/app/libs
# gomobile требует NDK; подхватываем системный Android SDK/NDK автоматически
export_android_env
AAR_OUT="android/app/libs/nbdbridge.aar"
NEED_GOMOBILE=0
[ ! -f "$AAR_OUT" ] && NEED_GOMOBILE=1
if [ "$NEED_GOMOBILE" -eq 0 ] && ! aar_looks_valid "$AAR_OUT"; then
    echo -e "${YELLOW}⚠${NC} Найден битый или пустой nbdbridge.aar. Пересобираю..."
    NEED_GOMOBILE=1
fi
if [ "$NEED_GOMOBILE" -eq 0 ]; then
    if find nbdbridge -name "*.go" -newer "$AAR_OUT" | head -1 | grep -q .; then
        NEED_GOMOBILE=1
    fi
fi
if [ "${FORCE_GOMOBILE:-0}" = "1" ]; then
    NEED_GOMOBILE=1
fi

if [ "$NEED_GOMOBILE" -eq 1 ]; then
    rm -f "$AAR_OUT"
    $GOMOBILE_CMD bind -target android -androidapi 24 -o "$AAR_OUT" ./nbdbridge || {
        echo -e "${RED}❌ gomobile bind не удался. Установите вручную:${NC}"
        echo "   go install golang.org/x/mobile/cmd/gomobile@latest"
        echo "   gomobile init"
        echo "   $GOMOBILE_CMD bind -target android -o $AAR_OUT ./nbdbridge"
        exit 1
    }
fi
if ! aar_looks_valid "$AAR_OUT"; then
    echo -e "${RED}❌ gomobile bind создал пустой или повреждённый nbdbridge.aar${NC}"
    echo "   Проверьте Android SDK/NDK и повторите сборку"
    exit 1
fi
if [ "$NEED_GOMOBILE" -eq 1 ]; then
    echo -e "${GREEN}✓${NC} nbdbridge.aar пересобран"
else
    echo "⚡ nbdbridge.aar уже актуален"
fi
echo ""

# 3. Fyne build для Go библиотеки
echo "🔨 Шаг 3/5: Сборка Go приложения (fyne)..."
ANDROID_SRC="$REPO_ROOT/cmd/android"
mkdir -p "$ANDROID_SRC/libs/arm64-v8a"
for so_file in android/jniLibs/arm64-v8a/*.so; do
    [ -f "$so_file" ] || continue
    sync_file_if_needed "$so_file" "$ANDROID_SRC/libs/arm64-v8a/$(basename "$so_file")"
done

if [ -z "${ANDROID_NDK_HOME:-}" ] || [ ! -d "$ANDROID_NDK_HOME" ]; then
    export_android_env
fi
if [ -z "${ANDROID_NDK_HOME:-}" ] || [ ! -d "$ANDROID_NDK_HOME" ]; then
    echo -e "${RED}❌ Android NDK не найден (fyne)${NC}"
    echo "   Установите NDK и задайте ANDROID_NDK_HOME"
    exit 1
fi
export ANDROID_NDK_HOME
export CGO_ENABLED=1
export GO111MODULE=on
export GOFLAGS="${GOFLAGS:-} -buildvcs=false"
if [ -d "$GOPATH_BIN" ] && [[ ":$PATH:" != *":$GOPATH_BIN:"* ]]; then
    export PATH="$GOPATH_BIN:$PATH"
fi

FYNE_INSTALL_VERSION="${FYNE_INSTALL_VERSION:-latest}"
FYNE_BIN="$(ensure_go_tool fyne "fyne.io/tools/cmd/fyne@${FYNE_INSTALL_VERSION}" 1)"
FYNE_APK="$REPO_ROOT/cmd/android/USBridge_Client.apk"
if [ ! -f "$FYNE_APK" ] && [ -f "$REPO_ROOT/USBridge_Client.apk" ]; then
    FYNE_APK="$REPO_ROOT/USBridge_Client.apk"
fi

find_apksigner() {
    local sdk_dir="$1"
    local candidate=""

    [ -n "$sdk_dir" ] || return 1
    [ -d "$sdk_dir/build-tools" ] || return 1

    candidate="$(find "$sdk_dir/build-tools" \
        \( -name apksigner -o -name apksigner.bat -o -name apksigner.cmd -o -name apksigner.exe \) \
        2>/dev/null | sort -V | tail -1)"
    [ -n "$candidate" ] || return 1

    printf '%s\n' "$candidate"
}

run_apksigner() {
    local apksigner_path="$1"
    shift

    case "$apksigner_path" in
        *.bat|*.cmd)
            "$apksigner_path" "$@"
            ;;
        *)
            "$apksigner_path" "$@"
            ;;
    esac
}

NEED_FYNE_BUILD=0
[ ! -f "$FYNE_APK" ] && NEED_FYNE_BUILD=1
if [ "${FORCE_FYNE:-0}" = "1" ]; then
    NEED_FYNE_BUILD=1
fi
if [ "$NEED_FYNE_BUILD" -eq 0 ] && tree_has_newer_files "$FYNE_APK" \
    "$REPO_ROOT/cmd/android" \
    "$REPO_ROOT/internal" \
    "$REPO_ROOT/nbdbridge" \
    "$REPO_ROOT/android/jniLibs/arm64-v8a"; then
    NEED_FYNE_BUILD=1
fi
if [ "$NEED_FYNE_BUILD" -eq 0 ] && path_is_newer_than "$FYNE_APK" \
    "$REPO_ROOT/go.mod" \
    "$REPO_ROOT/go.sum" \
    "$REPO_ROOT/FyneApp.toml" \
    "$REPO_ROOT/Icon.png" \
    "$AAR_OUT"; then
    NEED_FYNE_BUILD=1
fi

if [ "$NEED_FYNE_BUILD" -eq 1 ]; then
    cd "$ANDROID_SRC"
    "$FYNE_BIN" package \
        --target android/arm64 \
        --tags nosystray \
        --app-id com.usbridge.client \
        --name "USBridge Client" \
        --app-version "1.0.0" \
        --icon "$REPO_ROOT/Icon.png" \
        --release
    cd "$REPO_ROOT"
    if [ ! -f "$FYNE_APK" ] && [ -f "$REPO_ROOT/USBridge_Client.apk" ]; then
        FYNE_APK="$REPO_ROOT/USBridge_Client.apk"
    fi
    echo -e "${GREEN}✓${NC} Fyne APK пересобран"
else
    echo "⚡ Fyne APK уже актуален"
fi
echo ""

# 4. Извлечение .so из Fyne APK
echo "📂 Шаг 4/5: Извлечение нативных библиотек..."
if ! ensure_command_available unzip unzip; then
    echo -e "${RED}❌ unzip не найден${NC}"
    exit 1
fi
if [ ! -f "$FYNE_APK" ]; then
    echo -e "${RED}❌ Fyne APK не найден${NC}"
    exit 1
fi

mkdir -p android/app/src/main/jniLibs/arm64-v8a
EXTRACTED_SO="android/app/src/main/jniLibs/arm64-v8a/libUSBridge_Client.so"
NEED_EXTRACT_FYNE_SO=0
[ ! -f "$EXTRACTED_SO" ] && NEED_EXTRACT_FYNE_SO=1
if [ "$NEED_EXTRACT_FYNE_SO" -eq 0 ] && [ "$FYNE_APK" -nt "$EXTRACTED_SO" ]; then
    NEED_EXTRACT_FYNE_SO=1
fi

if [ "$NEED_EXTRACT_FYNE_SO" -eq 1 ]; then
    TEMP_APK=$(mktemp -d)
    unzip -q "$FYNE_APK" -d "$TEMP_APK"
    rm -f android/app/src/main/jniLibs/arm64-v8a/libUSBridge_Client.so
    rm -f android/app/src/main/jniLibs/arm64-v8a/libUSB_Bridge_Client.so
    cp "$TEMP_APK/lib/arm64-v8a/libUSBridge_Client.so" android/app/src/main/jniLibs/arm64-v8a/
    rm -rf "$TEMP_APK"
    echo -e "${GREEN}✓${NC} libUSBridge_Client.so извлечён из Fyne APK"
else
    echo "⚡ libUSBridge_Client.so уже актуален"
fi
for so_file in android/jniLibs/arm64-v8a/*.so; do
    [ -f "$so_file" ] || continue
    sync_file_if_needed "$so_file" "android/app/src/main/jniLibs/arm64-v8a/$(basename "$so_file")"
done
echo -e "${GREEN}✓${NC} Нативные библиотеки синхронизированы"
echo ""

# 5. Gradle сборка
echo "🔨 Шаг 5/5: Gradle assemble..."
# Kotlin 1.9 не поддерживает Java 25; используем JBR из Android Studio
if [ -z "$JAVA_HOME" ] || java -version 2>&1 | grep -q "version \"25"; then
    if [ -d "/Applications/Android Studio.app/Contents/jbr/Contents/Home" ]; then
        export JAVA_HOME="/Applications/Android Studio.app/Contents/jbr/Contents/Home"
    fi
fi
cd android
LOCAL_PROPERTIES="local.properties"
ANDROID_HOME_LOCAL="${ANDROID_HOME:-}"
ANDROID_NDK_HOME_LOCAL="${ANDROID_NDK_HOME:-}"

normalize_android_path() {
    local path_value="$1"
    if command -v cygpath >/dev/null 2>&1; then
        cygpath -m "$path_value"
    else
        printf '%s\n' "$path_value"
    fi
}

if [ -z "$ANDROID_HOME_LOCAL" ] || [ ! -d "$ANDROID_HOME_LOCAL" ]; then
    echo -e "${RED}❌ Android SDK не найден для Gradle${NC}"
    echo "   Ожидался ANDROID_HOME с валидным путем"
    exit 1
fi

LOCAL_PROPERTIES_CONTENT="sdk.dir=$(normalize_android_path "$ANDROID_HOME_LOCAL")"
if [ ! -f "$LOCAL_PROPERTIES" ] || [ "$(cat "$LOCAL_PROPERTIES" 2>/dev/null)" != "$LOCAL_PROPERTIES_CONTENT" ]; then
    printf '%s\n' "$LOCAL_PROPERTIES_CONTENT" > "$LOCAL_PROPERTIES"
fi

# Синхронизируем launcher icon с той же Icon.png, что используется в Windows build.
for d in mipmap-mdpi mipmap-hdpi mipmap-xhdpi mipmap-xxhdpi mipmap-xxxhdpi; do
    mkdir -p "app/src/main/res/$d"
    sync_file_if_needed "$REPO_ROOT/Icon.png" "app/src/main/res/$d/ic_launcher.png"
done

APK_OUT="app/build/outputs/apk/release/app-release-unsigned.apk"
NEED_GRADLE=0
[ ! -f "$APK_OUT" ] && NEED_GRADLE=1
if [ "${FORCE_GRADLE:-0}" = "1" ]; then
    NEED_GRADLE=1
fi
if [ "$NEED_GRADLE" -eq 0 ] && tree_has_newer_files "$APK_OUT" \
    "app/src/main" \
    "app/libs" \
    "app/src/main/jniLibs"; then
    NEED_GRADLE=1
fi
if [ "$NEED_GRADLE" -eq 0 ] && path_is_newer_than "$APK_OUT" \
    "build.gradle.kts" \
    "settings.gradle.kts" \
    "gradle.properties" \
    "app/build.gradle.kts" \
    "$LOCAL_PROPERTIES"; then
    NEED_GRADLE=1
fi

if [ "${FORCE_GRADLE_CLEAN:-0}" = "1" ]; then
    ./gradlew clean --no-daemon
    NEED_GRADLE=1
fi

if [ "$NEED_GRADLE" -eq 1 ]; then
    ./gradlew assembleRelease --no-daemon
    echo -e "${GREEN}✓${NC} Gradle APK пересобран"
else
    echo "⚡ Gradle APK уже актуален"
fi
cd "$REPO_ROOT"
echo ""

# Результат
APK_OUT="android/app/build/outputs/apk/release/app-release-unsigned.apk"
if [ -f "$APK_OUT" ]; then
    # Подписываем
    if ! ensure_command_available keytool keytool; then
        echo -e "${RED}❌ keytool не найден${NC}"
        exit 1
    fi
    KEYSTORE="$HOME/.android/debug.keystore"
    if [ ! -f "$KEYSTORE" ]; then
        mkdir -p "$HOME/.android"
        keytool -genkey -v -keystore "$KEYSTORE" -storepass android -alias androiddebugkey \
            -keypass android -keyalg RSA -keysize 2048 -validity 10000 \
            -dname "CN=Android Debug,O=Android,C=US"
    fi

    FINAL_APK="$DIST_DIR/USBridge_Client_gradle.apk"
    # Ищем apksigner: сначала в ANDROID_HOME (Linux), затем в стандартном пути macOS
    APKSIGNER=""
    if [ -n "$ANDROID_HOME" ]; then
        APKSIGNER="$(find_apksigner "$ANDROID_HOME" 2>/dev/null || true)"
    fi
    if [ -z "$APKSIGNER" ] && [ -d "$HOME/Library/Android/sdk" ]; then
        APKSIGNER="$(find_apksigner "$HOME/Library/Android/sdk" 2>/dev/null || true)"
    fi
    if [ -z "$APKSIGNER" ]; then
        echo -e "${RED}❌ apksigner не найден в Android SDK build-tools${NC}"
        echo "   Установите Android SDK Build-Tools или проверьте ANDROID_HOME"
        exit 1
    fi
    run_apksigner "$APKSIGNER" sign --ks "$KEYSTORE" --ks-pass pass:android \
        --key-pass pass:android --out "$FINAL_APK" "$APK_OUT"
    run_apksigner "$APKSIGNER" verify "$FINAL_APK" >/dev/null

    # apksigner может создать .idsig рядом с исходным/целевым файлом (зависит от версии).
    # Если idsig появился — складываем рядом с APK в dist/android.
    if [ -f "$FINAL_APK.idsig" ]; then
        :
    elif [ -f "$REPO_ROOT/$(basename "$FINAL_APK").idsig" ]; then
        mv -f "$REPO_ROOT/$(basename "$FINAL_APK").idsig" "$FINAL_APK.idsig" 2>/dev/null || true
    elif [ -f "$APK_OUT.idsig" ]; then
        cp -f "$APK_OUT.idsig" "$FINAL_APK.idsig" 2>/dev/null || true
    fi

    echo "=============================================="
    echo -e "${GREEN}🎉 Сборка завершена!${NC}"
    echo "   APK: $FINAL_APK"
    echo "   Размер: $(du -h "$FINAL_APK" | cut -f1)"
    echo ""
    echo "📱 Установка: adb install -r \"$FINAL_APK\""
    echo "=============================================="
else
    echo -e "${RED}❌ APK не найден: $APK_OUT${NC}"
    exit 1
fi
