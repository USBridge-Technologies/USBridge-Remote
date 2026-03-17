# Логи на Android

## Просмотр логов

На Android логи приложения пишутся в **logcat**. Подключите устройство по USB и выполните:

```bash
# Все логи приложения (теги USBridge и GStreamer-Static)
adb logcat USBridge:* GStreamer-Static:* *:S

# Только логи приложения USBridge (logrus)
adb logcat -s USBridge

# С очисткой буфера перед просмотром
adb logcat -c && adb logcat USBridge:* GStreamer-Static:*
```

## Теги

| Тег | Описание |
|-----|----------|
| `USBridge` | Логи из Go (logrus) — API, UI, видео, FRP |
| `GStreamer-Static` | Логи GStreamer — pipeline, RTP, декодер |

## Пример: отладка видео

```bash
adb logcat -c && adb logcat USBridge:* GStreamer-Static:* | grep -E "RTP|Кадр|GStreamer|video"
```

Ищите в логах:
- `📨 RTP: первый пакет получен!` — RTP доходит до pipeline
- `📊 Android: Кадр #1 обработан` — декодер выдал кадр
- `✅ [VIDEO] Шаг 7: Кадр отображён в UI` — кадр показан
