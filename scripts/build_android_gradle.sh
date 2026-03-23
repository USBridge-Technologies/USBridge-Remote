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
echo "📦 Шаг 1/5: GStreamer (dynamic .so)..."
"$SCRIPTS_DIR/build_gstreamer_dynamic_android.sh" 2>/dev/null || "$SCRIPTS_DIR/build_gstreamer_android.sh"
echo ""

# 2. gomobile bind для nbdbridge
echo "📦 Шаг 2/5: gomobile bind nbdbridge..."
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

if ! command -v java >/dev/null 2>&1; then
    echo -e "${RED}❌ Java не найден${NC}"
    echo "   Установите JDK, например: sudo apt-get install openjdk-21-jdk"
    exit 1
fi

# Убедимся, что ANDROID_HOME/ANDROID_NDK_HOME заданы до gomobile init
if [ -z "${ANDROID_HOME:-}" ] || [ ! -d "$ANDROID_HOME" ]; then
    if [ -d "$HOME/Android/Sdk" ]; then
        export ANDROID_HOME="$HOME/Android/Sdk"
    elif [ -d "$HOME/Android/sdk" ]; then
        export ANDROID_HOME="$HOME/Android/sdk"
    elif [ -d "/usr/lib/android-sdk" ]; then
        export ANDROID_HOME="/usr/lib/android-sdk"
    fi
fi
if [ -z "${ANDROID_NDK_HOME:-}" ] || [ ! -d "$ANDROID_NDK_HOME" ]; then
    if [ -n "${ANDROID_HOME:-}" ] && [ -d "$ANDROID_HOME/ndk" ]; then
        ANDROID_NDK_HOME="$(ls -d "$ANDROID_HOME"/ndk/*/ 2>/dev/null | head -1)"
        ANDROID_NDK_HOME="${ANDROID_NDK_HOME%/}"
    elif [ -d "/usr/lib/android-ndk" ]; then
        ANDROID_NDK_HOME="/usr/lib/android-ndk"
    fi
    [ -n "$ANDROID_NDK_HOME" ] && export ANDROID_NDK_HOME
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
# gomobile требует NDK с API 19-33; предпочитаем Android SDK NDK
if [ -d "$HOME/Library/Android/sdk/ndk" ]; then
    export ANDROID_NDK_HOME=$(ls -d "$HOME/Library/Android/sdk/ndk"/*/ 2>/dev/null | head -1)
fi
NEED_GOMOBILE=0
[ ! -f android/app/libs/nbdbridge.aar ] && NEED_GOMOBILE=1
if [ "$NEED_GOMOBILE" -eq 0 ]; then
    if find nbdbridge -name "*.go" -newer android/app/libs/nbdbridge.aar | head -1 | grep -q .; then
        NEED_GOMOBILE=1
    fi
fi
if [ "${FORCE_GOMOBILE:-0}" = "1" ]; then
    NEED_GOMOBILE=1
fi

if [ "$NEED_GOMOBILE" -eq 1 ]; then
    $GOMOBILE_CMD bind -target android -androidapi 24 -o android/app/libs/nbdbridge.aar ./nbdbridge || {
        echo -e "${RED}❌ gomobile bind не удался. Установите вручную:${NC}"
        echo "   go install golang.org/x/mobile/cmd/gomobile@latest"
        echo "   gomobile init"
        echo "   $GOMOBILE_CMD bind -target android -o android/app/libs/nbdbridge.aar ./nbdbridge"
        exit 1
    }
fi
echo -e "${GREEN}✓${NC} nbdbridge.aar готов"
echo ""

# 3. Fyne build для Go библиотеки
echo "🔨 Шаг 3/5: Сборка Go приложения (fyne)..."
ANDROID_SRC="$REPO_ROOT/cmd/android"
mkdir -p "$ANDROID_SRC/libs/arm64-v8a"
cp android/jniLibs/arm64-v8a/*.so "$ANDROID_SRC/libs/arm64-v8a/" 2>/dev/null || true

if [ -z "$ANDROID_NDK_HOME" ] || [ ! -d "$ANDROID_NDK_HOME" ]; then
    if [ -n "$ANDROID_HOME" ] && [ -d "$ANDROID_HOME/ndk" ]; then
        ANDROID_NDK_HOME="$(ls -d "$ANDROID_HOME"/ndk/*/ 2>/dev/null | head -1)"
        ANDROID_NDK_HOME="${ANDROID_NDK_HOME%/}"
    elif [ -d "$HOME/Library/Android/sdk/ndk" ]; then
        ANDROID_NDK_HOME="$(ls -d "$HOME/Library/Android/sdk/ndk"/*/ 2>/dev/null | head -1)"
        ANDROID_NDK_HOME="${ANDROID_NDK_HOME%/}"
    elif [ -d "/usr/lib/android-ndk" ]; then
        ANDROID_NDK_HOME="/usr/lib/android-ndk"
    fi
fi
if [ -z "$ANDROID_NDK_HOME" ] || [ ! -d "$ANDROID_NDK_HOME" ]; then
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

cd "$ANDROID_SRC"
"$FYNE_BIN" package \
    --target android/arm64 \
    --app-id com.usbridge.client \
    --name "USBridge Client" \
    --app-version "1.0.0" \
    --icon "$REPO_ROOT/Icon.png" \
    --release
cd "$REPO_ROOT"
echo ""

# 4. Извлечение .so из Fyne APK
echo "📂 Шаг 4/5: Извлечение нативных библиотек..."
FYNE_APK="$REPO_ROOT/cmd/android/USBridge_Client.apk"
if [ ! -f "$FYNE_APK" ]; then
    FYNE_APK="$REPO_ROOT/USBridge_Client.apk"
fi
if [ ! -f "$FYNE_APK" ]; then
    echo -e "${RED}❌ Fyne APK не найден${NC}"
    exit 1
fi

TEMP_APK=$(mktemp -d)
unzip -q "$FYNE_APK" -d "$TEMP_APK"

# Copy libUSBridge_Client.so into jniLibs
mkdir -p android/app/src/main/jniLibs/arm64-v8a
rm -f android/app/src/main/jniLibs/arm64-v8a/libUSBridge_Client.so
rm -f android/app/src/main/jniLibs/arm64-v8a/libUSB_Bridge_Client.so
cp "$TEMP_APK/lib/arm64-v8a/libUSBridge_Client.so" android/app/src/main/jniLibs/arm64-v8a/
cp android/jniLibs/arm64-v8a/*.so android/app/src/main/jniLibs/arm64-v8a/ 2>/dev/null || true
rm -rf "$TEMP_APK"
echo -e "${GREEN}✓${NC} Нативные библиотеки скопированы"
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

# Синхронизируем launcher icon с той же Icon.png, что используется в Windows build.
for d in mipmap-mdpi mipmap-hdpi mipmap-xhdpi mipmap-xxhdpi mipmap-xxxhdpi; do
    mkdir -p "app/src/main/res/$d"
    cp "$REPO_ROOT/Icon.png" "app/src/main/res/$d/ic_launcher.png" 2>/dev/null || true
done

./gradlew clean assembleRelease --no-daemon
cd "$REPO_ROOT"
echo ""

# Результат
APK_OUT="android/app/build/outputs/apk/release/app-release-unsigned.apk"
if [ -f "$APK_OUT" ]; then
    # Подписываем
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
    if [ -n "$ANDROID_HOME" ] && [ -d "$ANDROID_HOME/build-tools" ]; then
        APKSIGNER=$(find "$ANDROID_HOME/build-tools" -name apksigner 2>/dev/null | sort -V | tail -1)
    fi
    if [ -z "$APKSIGNER" ] && [ -d "$HOME/Library/Android/sdk/build-tools" ]; then
        APKSIGNER=$(find "$HOME/Library/Android/sdk/build-tools" -name apksigner 2>/dev/null | sort -V | tail -1)
    fi
    if [ -n "$APKSIGNER" ]; then
        "$APKSIGNER" sign --ks "$KEYSTORE" --ks-pass pass:android \
            --key-pass pass:android --out "$FINAL_APK" "$APK_OUT"
    else
        cp "$APK_OUT" "$FINAL_APK"
    fi

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
