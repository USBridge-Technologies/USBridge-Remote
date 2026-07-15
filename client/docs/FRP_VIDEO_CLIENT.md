# FRP Video — клиент RTP H.264

## Переход на RTP

- FFmpeg на Bridge выводит **RTP H.264** в `rtp://127.0.0.1:55000` вместо raw H.264
- RTP даёт корректную пакетизацию NAL и устойчивость к потере/переупорядочиванию пакетов
- GOP = FPS (IDR-кадр раз в секунду) для быстрого восстановления при потере пакетов

---

## GStreamer pipeline на клиенте (Mac/Linux)

**Низкая задержка (≈30–100 ms):** rtpjitterbuffer latency=50, buffer-size=128KB, frameChan=1

```bash
gst-launch-1.0 -q udpsrc port=55000 buffer-size=131072 caps="application/x-rtp,media=video,encoding-name=H264,payload=96" ! rtpjitterbuffer latency=50 faststart-min-packets=3 ! rtph264depay ! h264parse ! avdec_h264 ! autovideosink
```

С выводом в RGBA для приложения (fdsink):

```bash
gst-launch-1.0 -q udpsrc port=55000 buffer-size=131072 caps="application/x-rtp,media=video,encoding-name=H264,payload=96" ! rtpjitterbuffer latency=50 faststart-min-packets=3 ! rtph264depay ! h264parse ! vtdec ! videoscale ! video/x-raw,width=1920,height=1080 ! videoconvert ! video/x-raw,format=RGBA ! fdsink fd=1 sync=false
```

macOS использует `vtdec` (аппаратный), Linux — `avdec_h264`.

Для ещё меньшей задержки (нестабильнее при джиттере): `latency=30`.

---

## Вариант с очередью (меньше артефактов)

```bash
gst-launch-1.0 -q udpsrc port=55000 buffer-size=2097152 caps="application/x-rtp,media=video,encoding-name=H264,payload=96" ! queue ! rtph264depay ! h264parse ! avdec_h264 ! autovideosink
```

---

## Порядок запуска

1. Mac: Connect (FRP proxy `video_sudp` регистрируется)
2. Bridge: frpc с visitor подключается к frps
3. Mac: GStreamer стартует (udpsrc RTP на порту из config)
4. Mac: `POST /api/video/start`
5. Bridge: FFmpeg отправляет RTP на 127.0.0.1:55000

---

## Важно

- **RTCP:** FFmpeg RTP использует порт 55000 для RTP и 55001 для RTCP. FRP пробрасывает только 55000. Для одностороннего потока обычно достаточно.
- **payload=96** — типичный payload type для H.264 в RTP; при необходимости изменить на стороне FFmpeg и в caps.
