import codecs

filepath = "/Users/amir/Projects/usbridge/usbridge_client/internal/api/usb_client.go"
with codecs.open(filepath, "r", "utf-8") as f:
    content = f.read()

content = content.replace(
    '// 200 = синхронный успех, 202 = Accepted (монтирование в фоне)',
    '// 200 = synchronous success, 202 = Accepted (mounting in background)'
)
content = content.replace(
    '// Не возвращаем ошибку, так как это может быть нормально',
    '// Do not return error, as this might be normal'
)
content = content.replace(
    '// ConnectMouseWebSocket подключается к WebSocket for mouse control',
    '// ConnectMouseWebSocket connects to WebSocket for mouse control'
)
content = content.replace(
    'strings.Contains(apiResp.Message, "already runningо")) {',
    'strings.Contains(apiResp.Message, "already started")) {'
)
content = content.replace(
    'logrus.Info("🎥 Видео стриминг already running")',
    'logrus.Info("🎥 Video streaming is already running")'
)
content = content.replace(
    'logrus.Info("🎥 Видео стриминг запущен")',
    'logrus.Info("🎥 Video streaming started")'
)
content = content.replace(
    'logrus.Infof("📊 Прогресс: %.1f%% (%.2f MB / %.2f MB) | Скорость: %.2f MB/s | Осталось: %v",',
    'logrus.Infof("📊 Progress: %.1f%% (%.2f MB / %.2f MB) | Speed: %.2f MB/s | Remaining: %v",'
)
content = content.replace(
    '// buf = --{boundary}\\r\\n + headers + \\r\\n + \\r\\n--{boundary}--\\r\\n (для пустого файла)',
    '// buf = --{boundary}\\r\\n + headers + \\r\\n + \\r\\n--{boundary}--\\r\\n (for empty file)'
)
content = content.replace(
    'logrus.Infof("📊 File size: %.2f МБ", float64(fileSize)/1024/1024)',
    'logrus.Infof("📊 File size: %.2f MB", float64(fileSize)/1024/1024)'
)
content = content.replace(
    'logrus.Infof("📊 Read for sending: %.2f МБ", float64(written)/1024/1024)',
    'logrus.Infof("📊 Read for sending: %.2f MB", float64(written)/1024/1024)'
)
with codecs.open(filepath, "w", "utf-8") as f:
    f.write(content)
