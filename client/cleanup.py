import os
import re
import shutil

# 1. Update scripts/build_windows.sh
build_script = "scripts/build_windows.sh"
with open(build_script, "r", encoding="utf-8") as f:
    content = f.read()

# Replace Russian text and echo statements
replacements = {
    "Требования:": "Requirements:",
    "Обязательно:": "Mandatory:",
    "Для Moonlight (HW decode + WASAPI audio):": "For Moonlight (HW decode + WASAPI audio):",
    "Скачать:": "Download:",
    "Распаковать и указать:": "Extract and specify:",
    "Не найден downloader для": "Downloader not found for",
    "Нужен один из инструментов: curl, wget или powershell.": "Need one of: curl, wget or powershell.",
    "Не найден архиватор для": "Archiver not found for",
    "Нужен один из инструментов: unzip, bsdtar, tar или powershell.": "Need one of: unzip, bsdtar, tar or powershell.",
    "Не найден msiexec/powershell для распаковки": "msiexec/powershell not found for unpacking",
    "Проверка Go": "Check Go",
    "Go не найден! Установите:": "Go not found! Install:",
    "Проверка mingw-w64": "Check mingw-w64",
    "mingw-w64 не найден": "mingw-w64 not found",
    "Для кросс-сборки Windows на Linux нужен компилятор:": "For Windows cross-compilation on Linux you need a compiler:",
    "mingw-w64 найден": "mingw-w64 found",
    "Проверка pkg-config (Windows target)...": "Check pkg-config (Windows target)...",
    "pkg-config не найден": "pkg-config not found",
    "Установите:": "Install:",
    "FFMPEG_ROOT задан, но нет": "FFMPEG_ROOT is set, but no",
    "Для кросс-сборки Windows нужен PKG_CONFIG_LIBDIR или FFMPEG_ROOT": "For Windows cross-compilation you need PKG_CONFIG_LIBDIR or FFMPEG_ROOT",
    "Проверка fyne": "Check fyne",
    "fyne не найден, устанавливаю...": "fyne not found, installing...",
    "Иконка не найдена:": "Icon not found:",
    "Иконка:": "Icon:",
    "Компиляция...": "Compilation...",
    "DEBUG_CONSOLE=1: собираем консольную версию": "DEBUG_CONSOLE=1: building console version",
    "Найти или установить go-winres (встраивает иконку без ImageMagick и fyne)": "Find or install go-winres",
    "go-winres не найден, устанавливаю...": "go-winres not found, installing...",
    "Основное приложение (CGo + Fyne UI, встраиваем иконку через go-winres)": "Main app",
    "Сборка основного приложения": "Building main app",
    "Иконка встроена в основное приложение:": "Icon embedded into main app:",
    "go-winres недоступен — основное приложение будет без иконки": "go-winres unavailable - main app will be without icon",
    "Используем готовый app exe из кэша:": "Using ready app exe from cache:",
    "Создание dist": "Create dist folder",
    "Создание папки dist...": "Creating dist folder...",
    "Копирование FFmpeg DLLs": "Copying FFmpeg DLLs",
    "ОШИБКА: Не найдена зависимость": "ERROR: Dependency not found",
    "для": "for",
    "Копирование Moonlight runtime DLLs": "Copying Moonlight runtime DLLs",
    "ОШИБКА: Не найдена базовая библиотека": "ERROR: Base library not found",
    "Копирование QEMU": "Copying QEMU",
    "не найден в": "not found in",
    "Добавляем QEMU_BIN_DIR в пул поиска DLL": "Add QEMU_BIN_DIR to DLL search pool",
    "Перенос транзитивных зависимостей": "Move transitive dependencies",
    "Копирование dep walk": "Copying dep walk",
    "Проверка dep walk": "Check dep walk",
    "Подготовка README...": "Preparing README...",
    "Копирование Tailscale...": "Copying Tailscale...",
    "Удаление лаунчера": "Removing launcher",
    "Создание архива...": "Creating archive...",
    "Zip утилита не найдена. Папка не сжата.": "Zip utility not found. Folder not compressed.",
    "Сборка завершена с предупреждениями!": "Build completed with warnings!",
    "Некоторые необходимые DLL не были найдены.": "Some required DLLs were not found.",
    "Если вы используете MSYS2 UCRT64, убедитесь, что установлены все пакеты, например:": "If using MSYS2 UCRT64, ensure all packages are installed:",
    "Проверьте вывод выше, чтобы понять, какие именно библиотеки отсутствуют.": "Check output above to understand which libraries are missing.",
    "Сборка завершена!": "Build completed!",
    "Директория:": "Directory:",
    "Архив:": "Archive:",
    "APP_EXE_NAME=\"USBridge_Client_app.exe\"": "APP_EXE_NAME=\"USBridge_Client.exe\"",
    "BUILD_LDFLAGS=\"-H=windowsgui -X main.version=$VERSION\"": "BUILD_LDFLAGS=\"-H=windowsgui -X main.version=$VERSION -extldflags '-static-libgcc -static-libstdc++ -Wl,-Bstatic -lwinpthread -Wl,-Bdynamic'\""
}

for ru, en in replacements.items():
    content = content.replace(ru, en)

# Remove the launcher build block completely
content = re.sub(r'# 5b\. Лаунчер.*?\ncd "\$REPO_ROOT/cmd"\n', '', content, flags=re.DOTALL)
content = re.sub(r'BUILD_CACHE_LAUNCHER_RAW="\$BUILD_CACHE_DIR/USBridge_Client_launcher_raw\.exe"\nBUILD_CACHE_LAUNCHER_EXE="\$BUILD_CACHE_DIR/\$LAUNCHER_EXE_NAME"\n', '', content)
content = re.sub(r'cp "\$BUILD_CACHE_LAUNCHER_EXE" "\$DIST_WIN/\$LAUNCHER_EXE_NAME"\n.*?\(лаунчер с иконкой\)"\n', '', content)

with open(build_script, "w", encoding="utf-8") as f:
    f.write(content)

# Delete cmd/launcher
if os.path.exists("cmd/launcher"):
    shutil.rmtree("cmd/launcher")

# Replace ConnectToRTP in internal/service/moonlight_service.go
moonlight_service = "internal/service/moonlight_service.go"
if os.path.exists(moonlight_service):
    with open(moonlight_service, "r", encoding="utf-8") as f:
        mc = f.read()
    mc = mc.replace("ConnectToRTP", "ConnectToMoonlight")
    mc = mc.replace("Legacy RTP", "Legacy Moonlight")
    with open(moonlight_service, "w", encoding="utf-8") as f:
        f.write(mc)

for root, dirs, files in os.walk("internal"):
    for file in files:
        if file.endswith(".go"):
            filepath = os.path.join(root, file)
            with open(filepath, "r", encoding="utf-8") as f:
                c = f.read()
            if "ConnectToRTP" in c:
                c = c.replace("ConnectToRTP", "ConnectToMoonlight")
                with open(filepath, "w", encoding="utf-8") as f:
                    f.write(c)

print("Cleanup complete.")
