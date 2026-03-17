# GStreamer Dynamic Build для Android

Проект переведён на динамическую линковку GStreamer (.so) для аппаратного декодирования H.264.

## Сборка

1. **Скачанные архивы не удаляются** — `tmp_gstreamer/`, `gstreamer-android/` сохраняются.

2. **Динамическая сборка GStreamer** (~30–60 мин):
   ```bash
   scripts/build_gstreamer_dynamic_android.sh
   ```
   Создаёт `gstreamer-android-dynamic/` и копирует `.so` в `android/jniLibs/arm64-v8a/`.

3. **Полная сборка APK**:
   ```bash
   scripts/build_all_android.sh
   ```
   Вызовет `scripts/build_gstreamer_dynamic_android.sh`, затем сборку APK и упаковку.

## Структура

- `gstreamer-android-dynamic/` — результат динамической сборки (libgstreamer-full-1.0.so)
- `gstreamer-android/` — скачанный prebuilt 1.28.0 (содержит .a, для справки)
- `tmp_gstreamer/` — архив gstreamer-1.0-android-universal-1.28.0.tar.xz

## Отличия от статической сборки

- **CGO LDFLAGS**: только `-lgstreamer-full-1.0` + системные либы
- **Пути**: `gstreamer-android-dynamic/include`, `gstreamer-android-dynamic/lib`
- **gldownload** и GL-плагины должны работать корректно с динамической .so
