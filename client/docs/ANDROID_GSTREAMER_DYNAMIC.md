# GStreamer Dynamic Build для Android

> **Примечание:** GStreamer используется только для legacy RTP видео-режима.  
> **Moonlight streaming на Android использует `AMediaCodec` (NDK) для HW-декодирования H.264 и `AAudio` (NDK) для аудио** — системные API, не требующие GStreamer.  
> Эта документация актуальна только если вы используете non-Moonlight RTP видео-режим.

Проект поддерживает динамическую линковку GStreamer (.so) для legacy RTP видео-режима.

## Сборка

1. `scripts/build_android.sh` и `scripts/build_gstreamer_dynamic_android.sh` используют
   один и тот же Android GStreamer source build.

2. **Динамическая сборка GStreamer** (~30–60 мин):
   ```bash
   scripts/build_gstreamer_dynamic_android.sh
   ```
   Скрипт:
   - использует локальный `gstreamer/`, если он уже есть;
   - иначе автоматически клонирует GStreamer source tree версии `1.19.2`;
   - создаёт `gstreamer-android-dynamic/` и копирует `.so` в `android/jniLibs/arm64-v8a/`.

3. **Полная сборка APK**:
   ```bash
   scripts/build_android.sh
   ```
   Вызовет `scripts/build_gstreamer_dynamic_android.sh`, затем сборку APK и упаковку.

## Структура

- `gstreamer-android-dynamic/` — результат динамической сборки (libgstreamer-full-1.0.so)
- `gstreamer/` — исходники GStreamer, из которых собирается Android runtime
- `gstreamer-android/` и `tmp_gstreamer/` — legacy prebuilt-артефакты, для основной Android сборки больше не используются

## Отличия от статической сборки

- **CGO LDFLAGS**: только `-lgstreamer-full-1.0` + системные либы
- **Пути**: `gstreamer-android-dynamic/include`, `gstreamer-android-dynamic/lib`
- **gldownload** и GL-плагины должны работать корректно с динамической .so
