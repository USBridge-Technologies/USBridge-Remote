#!/bin/bash
# Сборка USBBridgeClient для macOS: бинарник + бандл с библиотеками
# Требования: Go, Homebrew GStreamer (brew install gstreamer gst-plugins-base gst-plugins-good gst-plugins-bad)

set -e

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

OUTPUT_NAME="USBBridgeClient"
DIST_ROOT="$REPO_ROOT/dist"
DIST_OS="$DIST_ROOT/macos"
if [ -d "$DIST_OS" ] && [ ! -w "$DIST_OS" ]; then
    echo -e "${RED}❌ Нет прав на запись в $DIST_OS${NC}"
    echo "   Исправьте права и повторите:"
    echo "   sudo chown -R \"$USER\":\"$USER\" \"$DIST_OS\""
    exit 1
fi
DIST_DIR="$DIST_OS/$OUTPUT_NAME"
BINARY_NAME="${OUTPUT_NAME}.bin"

# Цвета для вывода
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${GREEN}🍎 Сборка USBBridgeClient для macOS${NC}"

# 1. Проверка GStreamer
echo -e "\n${YELLOW}📦 Проверка зависимостей...${NC}"
GST_LAUNCH=""
for p in "gst-launch-1.0" "/opt/homebrew/bin/gst-launch-1.0" "/usr/local/bin/gst-launch-1.0"; do
    if command -v "$p" &>/dev/null || [ -x "$p" ]; then
        GST_LAUNCH="$p"
        break
    fi
done

if [ -z "$GST_LAUNCH" ]; then
    echo -e "${RED}❌ GStreamer не найден. Установите:${NC}"
    echo "   brew install gstreamer gst-plugins-base gst-plugins-good gst-plugins-bad"
    exit 1
fi
echo -e "   gst-launch: $GST_LAUNCH"

# 2. Сборка бинарника
echo -e "\n${YELLOW}🔨 Компиляция...${NC}"
# Исправление линковки: pkg-config может возвращать устаревший путь (1.26.10),
# а GStreamer обновлён (1.28.x). Добавляем -L/opt/homebrew/lib для поиска libs.
HOMEBREW_PREFIX="/opt/homebrew"
[ -d "/usr/local/lib" ] && [ ! -d "/opt/homebrew/lib" ] && HOMEBREW_PREFIX="/usr/local"
export CGO_ENABLED=1
export CGO_LDFLAGS="-L${HOMEBREW_PREFIX}/lib ${CGO_LDFLAGS:-}"
export CGO_CPPFLAGS="-I${HOMEBREW_PREFIX}/include ${CGO_CPPFLAGS:-}"
# Подавить предупреждение format-security из зависимости go-gst (gst_debug.go)
export CGO_CFLAGS="${CGO_CFLAGS:-} -Wno-format-security"
go build -ldflags="-s -w" -o "$BINARY_NAME" ./cmd/main.go
echo -e "${GREEN}   ✅ Бинарник: $BINARY_NAME${NC}"

# 3. Создание dist с бандлом
echo -e "\n${YELLOW}📁 Создание бандла...${NC}"
rm -rf "$DIST_DIR"
mkdir -p "$DIST_DIR"

# Копируем бинарник
cp "$BINARY_NAME" "$DIST_DIR/"
chmod +x "$DIST_DIR/$BINARY_NAME"

# Копируем config если есть
[ -f config.yaml ] && cp config.yaml "$DIST_DIR/"

# 4. Создаём run-скрипт с правильным окружением
cat > "$DIST_DIR/run.sh" << 'RUNSCRIPT'
#!/bin/bash
# Запуск с окружением для GStreamer
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$DIR"

# Добавляем Homebrew в PATH для gst-launch
export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"

# Библиотеки GStreamer (бинарник линкуется на Homebrew libs)
if [ -d "/opt/homebrew/lib" ]; then
    export DYLD_FALLBACK_LIBRARY_PATH="/opt/homebrew/lib:${DYLD_FALLBACK_LIBRARY_PATH:-}"
elif [ -d "/usr/local/lib" ]; then
    export DYLD_FALLBACK_LIBRARY_PATH="/usr/local/lib:${DYLD_FALLBACK_LIBRARY_PATH:-}"
fi

exec "./USBBridgeClient.bin" "$@"
RUNSCRIPT
chmod +x "$DIST_DIR/run.sh"

# 5. Создаём README для dist
cat > "$DIST_DIR/README.txt" << 'README'
USBBridgeClient для macOS
=========================

Запуск:
  ./run.sh          — через скрипт (рекомендуется)
  ./USBBridgeClient.bin — напрямую (если GStreamer в PATH)

Требования:
  - GStreamer: brew install gstreamer gst-plugins-base gst-plugins-good gst-plugins-bad
  - macOS 10.15+

Конфигурация: config.yaml (в этой папке или ~/.config/usbridge-client/)
README

echo -e "\n${GREEN}✅ Сборка завершена!${NC}"
echo -e "   Результат: $DIST_DIR/"
echo -e "   Запуск:    cd $DIST_DIR && ./run.sh"
echo ""
