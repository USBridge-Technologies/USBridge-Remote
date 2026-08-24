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

# Distribution channel: which flavor (client/android/app/build.gradle.kts)
# and Go build tag (client/internal/update/update_disabled.go) this build
# gets.
#   - Default (no flag): "market" — no self-update, no
#     REQUEST_INSTALL_PACKAGES permission. This is what Play Store and
#     F-Droid must get.
#   - --self-update / WITH_SELF_UPDATE=1: "direct" — today's behavior,
#     unchanged. Used for the off-market APK published to GitHub Releases;
#     CI opts into it explicitly for that job only.
# --aab / WITH_AAB=1 additionally builds the .aab (Play Store upload) —
# only meaningful for the market flavor, since that's the only flavor with
# a signingConfig attached (see build.gradle.kts).
WITH_SELF_UPDATE="${WITH_SELF_UPDATE:-0}"
WITH_AAB="${WITH_AAB:-0}"
for arg in "$@"; do
    case "$arg" in
        --self-update)    WITH_SELF_UPDATE=1 ;;
        --no-self-update) WITH_SELF_UPDATE=0 ;;
        --aab)            WITH_AAB=1 ;;
    esac
done
if [ "$WITH_SELF_UPDATE" = "1" ]; then
    GRADLE_FLAVOR="direct"
else
    GRADLE_FLAVOR="market"
fi
GRADLE_FLAVOR_CAP="$(printf '%s' "${GRADLE_FLAVOR:0:1}" | tr '[:lower:]' '[:upper:]')${GRADLE_FLAVOR:1}"

echo "=============================================="
echo "  USBridge Client - Gradle build (camera)"
echo "  Distribution: $GRADLE_FLAVOR (self-update: $WITH_SELF_UPDATE, aab: $WITH_AAB)"
echo "=============================================="
echo ""

# Google Play requires every native library in the app to support 16 KB
# memory pages (developer.android.com/guide/practices/page-sizes) — recent
# NDKs emit 16 KB-aligned ELF LOAD segments by default, but which NDK
# actually gets picked (setup_android_ndk_toolchain_env / android_env.sh's
# auto-detection, or CI's own preset ANDROID_NDK_HOME) isn't guaranteed to
# be one of those, and silently regressing this per-runner isn't
# acceptable for a Play Store upload. Pass the flag explicitly everywhere
# a .so ends up in jniLibs (gomobile bind's androidbridge.aar and the
# fyne-packaged main library), so it's correct regardless of NDK revision.
NDK_PAGE_SIZE_LDFLAGS="-Wl,-z,max-page-size=16384"

# 1. gomobile bind for androidbridge
echo "📦 Step 1/4: gomobile bind androidbridge..."

# No separate Tailscale CLI/daemon .so build here (there used to be one,
# cross-compiling tailscale.com/cmd/tailscale + cmd/tailscaled into
# libtailscale.so/libtailscaled.so and shipping them in jniLibs) --
# androidbridge talks Tailscale entirely through the embedded tsnet.Server
# library (internal/service/tailscale_service.go, the same tsnet the
# desktop client and usbridge-agent both already use), never by exec'ing or
# JNI-calling an external tailscale/tailscaled process. Nothing in this
# repo (Go, Kotlin, or the Android manifest) ever referenced those two
# binaries after they were built -- pure dead weight bloating every APK by
# their combined size for a code path that never ran. See internal/service/
# tailscale_deps_stub.go's own removal for the go.mod side of this cleanup.
if ! ensure_command_available go Go; then
    echo -e "${RED}❌ Go not found${NC}"
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

    echo -e "${YELLOW}Installing $tool...${NC}" >&2
    # This tool (fyne, gomobile, ...) is a build-time binary that must run on
    # *this* CI/dev machine, not the Android target -- but by the time this is
    # called for fyne, setup_android_ndk_toolchain_env has already exported
    # CC/CGO_ENABLED/etc. for cross-compiling the app itself (GOOS=android
    # GOARCH=arm64). Left inherited, `go install` tries to build this tool's
    # cgo runtime bits (e.g. runtime/cgo's host asm) using the NDK's
    # arm64-targeted clang, which can't assemble the host's own architecture
    # ("gcc_amd64.S: unknown token in expression" on an amd64 CI runner) --
    # explicitly pin GOOS/GOARCH back to the host and drop the cross CC/CXX
    # for this one invocation so it always builds for the host regardless of
    # what's exported around it.
    if ! env -u CC -u CXX -u CGO_LDFLAGS GOOS="$(go env GOHOSTOS)" GOARCH="$(go env GOHOSTARCH)" go install "$pkg"; then
        echo -e "${RED}❌ Failed to install $tool${NC}" >&2
        echo "   Check network access and the command: go install $pkg" >&2
        exit 1
    fi

    tool_path="$(find_go_tool "$tool" 2>/dev/null || true)"
    if [ -z "$tool_path" ]; then
        echo -e "${RED}❌ $tool not found after installation${NC}" >&2
        echo "   Check PATH or use: $(go env GOPATH)/bin/$tool" >&2
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
    echo -e "${RED}❌ Java not found${NC}"
    exit 1
fi

# Make sure ANDROID_HOME/ANDROID_NDK_HOME are set before gomobile init
export_android_env
if ! ensure_android_sdk_package "platforms;android-26" "platforms/android-26"; then
    echo -e "${RED}❌ Android SDK platform android-26 not found${NC}"
    echo "   Install via sdkmanager: platforms;android-26"
    exit 1
fi

"$GOMOBILE_CMD" init || {
    echo -e "${RED}❌ gomobile init failed${NC}"
    echo "   Check ANDROID_HOME/ANDROID_NDK_HOME and the installed JDK"
    exit 1
}

GOBIND_CMD="$(find_go_tool gobind 2>/dev/null || true)"
if [ -z "$GOBIND_CMD" ]; then
    GOBIND_CMD="$(ensure_go_tool gobind golang.org/x/mobile/cmd/gobind@latest)"
fi

if [ -z "$GOBIND_CMD" ]; then
    echo -e "${RED}❌ gobind not found after gomobile init${NC}"
    echo "   Try: go install golang.org/x/mobile/cmd/gobind@latest"
    exit 1
fi

mkdir -p android/app/libs
# gomobile requires the NDK; pick up the system Android SDK/NDK automatically
export_android_env
# gomobile bind's -target android always produces a c-shared-style JNI
# library (external linking, no -linkmode flag needed to force it, unlike
# the plain tailscale/tailscaled executables above) -- CGO_LDFLAGS is all
# it needs. Exported (not just set) so it also reaches the later fyne
# package step below, which appends its own -L flag onto this same
# variable rather than overwriting it.
export CGO_LDFLAGS="${CGO_LDFLAGS:-} $NDK_PAGE_SIZE_LDFLAGS"
AAR_OUT="android/app/libs/androidbridge.aar"
NEED_GOMOBILE=0
[ ! -f "$AAR_OUT" ] && NEED_GOMOBILE=1
if [ "$NEED_GOMOBILE" -eq 0 ] && ! aar_looks_valid "$AAR_OUT"; then
    echo -e "${YELLOW}⚠${NC} Found a broken or empty androidbridge.aar. Rebuilding..."
    NEED_GOMOBILE=1
fi
if [ "$NEED_GOMOBILE" -eq 0 ]; then
    if find androidbridge -name "*.go" -newer "$AAR_OUT" | head -1 | grep -q .; then
        NEED_GOMOBILE=1
    fi
fi
if [ "${FORCE_GOMOBILE:-0}" = "1" ]; then
    NEED_GOMOBILE=1
fi

if [ "$NEED_GOMOBILE" -eq 1 ]; then
    rm -f "$AAR_OUT"
    # -target android/arm64, not just "android" (which silently builds all
    # four ABIs -- arm/arm64/x86/x86_64 -- since gomobile defaults to
    # every ABI it knows when none is named). This project only ever
    # ships arm64-v8a; the other three were pure wasted build time here.
    $GOMOBILE_CMD bind -target android/arm64 -androidapi 26 -o "$AAR_OUT" ./androidbridge || {
        echo -e "${RED}❌ gomobile bind failed. Install it manually:${NC}"
        echo "   go install golang.org/x/mobile/cmd/gomobile@latest"
        echo "   gomobile init"
        echo "   $GOMOBILE_CMD bind -target android/arm64 -o $AAR_OUT ./androidbridge"
        exit 1
    }
fi
if ! aar_looks_valid "$AAR_OUT"; then
    echo -e "${RED}❌ gomobile bind produced an empty or corrupt androidbridge.aar${NC}"
    echo "   Check the Android SDK/NDK and retry the build"
    exit 1
fi
if [ "$NEED_GOMOBILE" -eq 1 ]; then
    echo -e "${GREEN}✓${NC} androidbridge.aar rebuilt"
else
    echo "⚡ androidbridge.aar is already up to date"
fi
echo ""

# 2. Fyne build for the Go library
echo "🔨 Step 2/4: Building the Go application (fyne)..."
ANDROID_SRC="$REPO_ROOT/cmd/android"
# cmd/android/main.go embeds this copy (go:embed can't reach a parent dir) to
# show the real app version in-UI; keep it in sync with the source of truth.
cp "$REPO_ROOT/VERSION" "$ANDROID_SRC/VERSION"
mkdir -p "$ANDROID_SRC/libs/arm64-v8a"

if [ -z "${ANDROID_NDK_HOME:-}" ] || [ ! -d "$ANDROID_NDK_HOME" ]; then
    export_android_env
fi
if [ -z "${ANDROID_NDK_HOME:-}" ] || [ ! -d "$ANDROID_NDK_HOME" ]; then
    echo -e "${RED}❌ Android NDK not found (fyne)${NC}"
    echo "   Install the NDK and set ANDROID_NDK_HOME"
    exit 1
fi
export ANDROID_NDK_HOME
export CGO_LDFLAGS_ALLOW=".*"
export CGO_ENABLED=1
export GO111MODULE=on
export GOFLAGS="${GOFLAGS:-} -buildvcs=false"

# Without an explicit CC, Go's own cgo cross-compilation for GOOS=android
# GOARCH=arm64 resolves its default clang wrapper to API 21
# (aarch64-linux-android21-clang). NDK clang gates a handful of libraries by
# their own minimum API level as part of its target triple, independently of
# any extra -L search path: libaaudio.so (min API 26) and libvulkan.so (min
# API 24) are NOT resolvable through a 21-targeted clang/lld invocation even
# when their directory is manually added to CGO_LDFLAGS below — only
# retargeting the compiler itself to API 26 (matching this project's actual
# minSdkVersion, and the same api_level already used for the Tailscale build
# a few steps above) makes clang's own availability check pass. Previously
# only the extra -L was added here, which fixed nothing: the link still
# failed with "unable to find library -laaudio"/"-lvulkan" even though the
# files plainly exist at that path.
setup_android_ndk_toolchain_env "$ANDROID_NDK_HOME" 26

# Belt-and-suspenders: also add the API 26 sysroot lib dir explicitly, in
# case a future Go toolchain version stops honoring CC for this and needs
# the extra search path again.
_NDK_PREBUILT="$(find "${ANDROID_NDK_HOME}/toolchains/llvm/prebuilt" \
    -mindepth 1 -maxdepth 1 -type d 2>/dev/null | sort | head -1)"
_NDK_API26_LIB="${_NDK_PREBUILT}/sysroot/usr/lib/aarch64-linux-android/26"
if [ -d "${_NDK_API26_LIB}" ]; then
    export CGO_LDFLAGS="${CGO_LDFLAGS:-} -L${_NDK_API26_LIB}"
fi
unset _NDK_PREBUILT _NDK_API26_LIB
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

# Compose build tags: base + noselfupdate unless this is the
# self-update-enabled "direct" build (see GRADLE_FLAVOR above) — this is
# what actually removes internal/update's network/install code from the
# compiled binary, not just the Gradle flavor's manifest permission.
FYNE_TAGS="nosystray"
if [ "$WITH_SELF_UPDATE" != "1" ]; then
    FYNE_TAGS="$FYNE_TAGS,noselfupdate"
fi

# Stamp file to invalidate the cache when the build tags change
FYNE_TAGS_STAMP="$REPO_ROOT/.build-cache/android/fyne_tags.stamp"
mkdir -p "$(dirname "$FYNE_TAGS_STAMP")"
PREV_FYNE_TAGS="$(cat "$FYNE_TAGS_STAMP" 2>/dev/null || true)"

NEED_FYNE_BUILD=0
[ ! -f "$FYNE_APK" ] && NEED_FYNE_BUILD=1
if [ "${FORCE_FYNE:-0}" = "1" ]; then
    NEED_FYNE_BUILD=1
fi
if [ "$NEED_FYNE_BUILD" -eq 0 ] && [ "$PREV_FYNE_TAGS" != "$FYNE_TAGS" ]; then
    echo "   Build tags changed ($PREV_FYNE_TAGS → $FYNE_TAGS) — rebuilding"
    NEED_FYNE_BUILD=1
fi
if [ "$NEED_FYNE_BUILD" -eq 0 ] && tree_has_newer_files "$FYNE_APK" \
    "$REPO_ROOT/cmd/android" \
    "$REPO_ROOT/internal" \
    "$REPO_ROOT/androidbridge" \
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
    VERSION=$(cat "$REPO_ROOT/VERSION" 2>/dev/null || echo "1.0.0")
    cd "$ANDROID_SRC"
    "$FYNE_BIN" package \
        --target android/arm64 \
        --tags "$FYNE_TAGS" \
        --app-id io.usbridge.client \
        --name "USBridge Client" \
        --app-version "$VERSION" \
        --icon "$REPO_ROOT/Icon.png" \
        --release
    cd "$REPO_ROOT"
    printf '%s' "$FYNE_TAGS" > "$FYNE_TAGS_STAMP"
    if [ ! -f "$FYNE_APK" ] && [ -f "$REPO_ROOT/USBridge_Client.apk" ]; then
        FYNE_APK="$REPO_ROOT/USBridge_Client.apk"
    fi
    echo -e "${GREEN}✓${NC} Fyne APK rebuilt (tags: $FYNE_TAGS)"
else
    echo "⚡ Fyne APK is already up to date (tags: $FYNE_TAGS)"
fi
echo ""

# 3. Extracting .so from the Fyne APK
echo "📂 Step 3/4: Extracting native libraries..."
if ! ensure_command_available unzip unzip; then
    echo -e "${RED}❌ unzip not found${NC}"
    exit 1
fi
if [ ! -f "$FYNE_APK" ]; then
    echo -e "${RED}❌ Fyne APK not found${NC}"
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
    echo -e "${GREEN}✓${NC} libUSBridge_Client.so extracted from Fyne APK"
else
    echo "⚡ libUSBridge_Client.so is already up to date"
fi
echo -e "${GREEN}✓${NC} Native libraries synchronized"
echo ""

# 4. Gradle build
echo "🔨 Step 4/4: Gradle assemble..."
# Kotlin 1.9 does not support Java 25; use the JBR bundled with Android Studio
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
    echo -e "${RED}❌ Android SDK not found for Gradle${NC}"
    echo "   Expected ANDROID_HOME to be set to a valid path"
    exit 1
fi

LOCAL_PROPERTIES_CONTENT="sdk.dir=$(normalize_android_path "$ANDROID_HOME_LOCAL")"
if [ ! -f "$LOCAL_PROPERTIES" ] || [ "$(cat "$LOCAL_PROPERTIES" 2>/dev/null)" != "$LOCAL_PROPERTIES_CONTENT" ]; then
    printf '%s\n' "$LOCAL_PROPERTIES_CONTENT" > "$LOCAL_PROPERTIES"
fi

# Synchronize the launcher icon with the same Icon.png used in the Windows build.
for d in mipmap-mdpi mipmap-hdpi mipmap-xhdpi mipmap-xxhdpi mipmap-xxxhdpi; do
    mkdir -p "app/src/main/res/$d"
    sync_file_if_needed "$REPO_ROOT/Icon.png" "app/src/main/res/$d/ic_launcher.png"
done

APK_OUT="app/build/outputs/apk/$GRADLE_FLAVOR/release/app-$GRADLE_FLAVOR-release-unsigned.apk"
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
    ./gradlew "assemble${GRADLE_FLAVOR_CAP}Release" --no-daemon
    echo -e "${GREEN}✓${NC} Gradle APK rebuilt"
else
    echo "⚡ Gradle APK is already up to date"
fi

AAB_OUT=""
if [ "$WITH_AAB" = "1" ]; then
    AAB_OUT="app/build/outputs/bundle/${GRADLE_FLAVOR}Release/app-${GRADLE_FLAVOR}-release.aab"
    NEED_BUNDLE=0
    [ ! -f "$AAB_OUT" ] && NEED_BUNDLE=1
    [ "$NEED_GRADLE" -eq 1 ] && NEED_BUNDLE=1
    if [ "${FORCE_GRADLE:-0}" = "1" ]; then
        NEED_BUNDLE=1
    fi

    if [ "$NEED_BUNDLE" -eq 1 ]; then
        ./gradlew "bundle${GRADLE_FLAVOR_CAP}Release" --no-daemon
        echo -e "${GREEN}✓${NC} Gradle AAB rebuilt"
    else
        echo "⚡ Gradle AAB is already up to date"
    fi
fi
cd "$REPO_ROOT"
echo ""

# Result
APK_OUT="android/app/build/outputs/apk/$GRADLE_FLAVOR/release/app-$GRADLE_FLAVOR-release-unsigned.apk"
if [ -f "$APK_OUT" ]; then
    # Sign it
    if ! ensure_command_available keytool keytool; then
        echo -e "${RED}❌ keytool not found${NC}"
        exit 1
    fi
    # Use a real release keystore when one is provided (e.g. CI, decoded from
    # ANDROID_KEYSTORE_BASE64 into ANDROID_KEYSTORE_PATH) so every build is
    # signed with the same key. Falls back to the auto-generated debug
    # keystore for local dev builds, but that keystore is unique per machine
    # and gets regenerated on any ephemeral runner — never use it for a build
    # meant to be a signature-stable upgrade path (e.g. a release users install
    # over a previous version).
    KEYSTORE="${ANDROID_KEYSTORE_PATH:-$HOME/.android/debug.keystore}"
    KEYSTORE_PASS="${ANDROID_KEYSTORE_PASSWORD:-android}"
    KEY_ALIAS="${ANDROID_KEY_ALIAS:-androiddebugkey}"
    KEY_PASS="${ANDROID_KEY_PASSWORD:-android}"
    if [ -z "${ANDROID_KEYSTORE_PATH:-}" ] && [ ! -f "$KEYSTORE" ]; then
        mkdir -p "$HOME/.android"
        keytool -genkey -v -keystore "$KEYSTORE" -storepass android -alias androiddebugkey \
            -keypass android -keyalg RSA -keysize 2048 -validity 10000 \
            -dname "CN=Android Debug,O=Android,C=US"
    fi
    if [ -n "${ANDROID_KEYSTORE_PATH:-}" ] && [ ! -f "$KEYSTORE" ]; then
        echo -e "${RED}❌ ANDROID_KEYSTORE_PATH is set but the file does not exist: $KEYSTORE${NC}"
        exit 1
    fi

    APK_VERSION=$(cat "$REPO_ROOT/VERSION" 2>/dev/null || echo "1.0.0")
    # Distinguishing infix per flavor so the two are never confused at a
    # glance on the releases page, and so scripts/sign_update_manifest.go
    # can tell them apart (it skips anything with "-market-" in the name,
    # the same way it already skips "-iOS-" — the self-update manifest
    # must only ever point at the "-selfupdate-" asset). The self-update
    # client itself doesn't care about the filename either way — it reads
    # whatever name manifest-client.json records, generated fresh each
    # release (see scripts/sign_update_manifest.go), so renaming this
    # doesn't break already-installed clients' next update check.
    if [ "$GRADLE_FLAVOR" = "direct" ]; then
        FINAL_APK="$DIST_DIR/USBridgeClient-Android-arm64-selfupdate-${APK_VERSION}.apk"
    else
        FINAL_APK="$DIST_DIR/USBridgeClient-Android-arm64-market-${APK_VERSION}.apk"
    fi
    # Look for apksigner: first in ANDROID_HOME (Linux), then in the standard macOS path
    APKSIGNER=""
    if [ -n "$ANDROID_HOME" ]; then
        APKSIGNER="$(find_apksigner "$ANDROID_HOME" 2>/dev/null || true)"
    fi
    if [ -z "$APKSIGNER" ] && [ -d "$HOME/Library/Android/sdk" ]; then
        APKSIGNER="$(find_apksigner "$HOME/Library/Android/sdk" 2>/dev/null || true)"
    fi
    if [ -z "$APKSIGNER" ]; then
        echo -e "${RED}❌ apksigner not found in Android SDK build-tools${NC}"
        echo "   Install Android SDK Build-Tools or check ANDROID_HOME"
        exit 1
    fi
    run_apksigner "$APKSIGNER" sign --ks "$KEYSTORE" --ks-pass "pass:$KEYSTORE_PASS" \
        --ks-key-alias "$KEY_ALIAS" --key-pass "pass:$KEY_PASS" --out "$FINAL_APK" "$APK_OUT"

    # Verify the signature the same way the macOS/iOS jobs verify codesign:
    # confirm apksigner accepts it, then print the signer cert fingerprint so
    # CI logs make it obvious whether this build used the expected release
    # key or fell back to a throwaway debug key (fingerprint would differ
    # build-to-build in that case).
    run_apksigner "$APKSIGNER" verify --print-certs "$FINAL_APK"
    KEYTOOL_LIST="$(keytool -printcert -jarfile "$FINAL_APK" 2>&1)" || {
        echo -e "${RED}❌ Could not read signer certificate from $FINAL_APK${NC}"
        exit 1
    }
    echo "$KEYTOOL_LIST" | grep -E "Owner|SHA256"
    if [ -z "${ANDROID_KEYSTORE_PATH:-}" ]; then
        echo -e "${YELLOW}⚠ signed with the auto-generated debug keystore — fingerprint will change on every fresh runner${NC}"
    fi

    # apksigner may create a .idsig next to the source/target file (depends on version).
    # If idsig shows up — place it next to the APK in dist/android.
    if [ -f "$FINAL_APK.idsig" ]; then
        :
    elif [ -f "$REPO_ROOT/$(basename "$FINAL_APK").idsig" ]; then
        mv -f "$REPO_ROOT/$(basename "$FINAL_APK").idsig" "$FINAL_APK.idsig" 2>/dev/null || true
    elif [ -f "$APK_OUT.idsig" ]; then
        cp -f "$APK_OUT.idsig" "$FINAL_APK.idsig" 2>/dev/null || true
    fi

    FINAL_AAB=""
    if [ "$WITH_AAB" = "1" ]; then
        AAB_OUT="android/$AAB_OUT"
        if [ ! -f "$AAB_OUT" ]; then
            echo -e "${RED}❌ AAB not found: $AAB_OUT${NC}"
            exit 1
        fi
        if ! ensure_command_available jarsigner jarsigner; then
            echo -e "${RED}❌ jarsigner not found (part of the JDK)${NC}"
            exit 1
        fi
        FINAL_AAB="$DIST_DIR/USBridgeClient-Android-arm64-market-${APK_VERSION}.aab"
        cp -f "$AAB_OUT" "$FINAL_AAB"
        # Gradle's bundle task produces an unsigned .aab (no signingConfig
        # attached to either flavor — see build.gradle.kts). apksigner is
        # APK-only, so sign this one with jarsigner directly instead, same
        # keystore/alias/passwords as the APK above; Play accepts a
        # JAR-signed bundle.
        jarsigner -keystore "$KEYSTORE" -storepass "$KEYSTORE_PASS" \
            -keypass "$KEY_PASS" "$FINAL_AAB" "$KEY_ALIAS"
        # No -strict: jarsigner treats a self-signed cert (which is exactly
        # what every Android app-signing key is — there's no CA chain to a
        # trusted root, apksigner doesn't care either) as a verification
        # "error" and -strict turns that into a nonzero exit. Plain -verify
        # still prints "jar verified" and fails loudly on anything that's
        # actually wrong (bad password, tampered file, ...).
        jarsigner -verify "$FINAL_AAB"
        if [ "$GRADLE_FLAVOR" != "market" ]; then
            echo -e "${YELLOW}⚠ built an .aab for the \"$GRADLE_FLAVOR\" flavor — --aab is meant for the market flavor (Play Store).${NC}"
        fi
    fi

    echo "=============================================="
    echo -e "${GREEN}🎉 Build complete!${NC}"
    echo "   APK: $FINAL_APK"
    echo "   Size: $(du -h "$FINAL_APK" | cut -f1)"
    if [ -n "$FINAL_AAB" ]; then
        echo "   AAB: $FINAL_AAB"
        echo "   Size: $(du -h "$FINAL_AAB" | cut -f1)"
    fi
    echo ""
    echo "📱 Install: adb install -r \"$FINAL_APK\""
    echo "=============================================="
else
    echo -e "${RED}❌ APK not found: $APK_OUT${NC}"
    exit 1
fi
