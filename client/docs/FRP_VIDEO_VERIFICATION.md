# FRP Video — конфигурация

## Архитектура (как NBD)

- **Mac (клиент):** proxy `video_sudp` — публикует localPort 55000, GStreamer udpsrc слушает RTP на 55000
- **Bridge:** visitor — биндится на 55000, принимает RTP от FFmpeg, пересылает в туннель

Поток: `Bridge (FFmpeg RTP→55000) → visitor → QUIC → Mac proxy → 127.0.0.1:55000 → GStreamer (rtph264depay)`

---

## Конфигурация на Bridge (Radxa)

Bridge должен иметь **visitor** (не proxy):

```yaml
# frpc.yaml на Bridge
visitors:
  - name: "video_udp_to_client"
    type: "sudp"
    serverName: "video_sudp"   # proxy на Mac
    secretKey: "sk-video"
    bindAddr: "127.0.0.1"
    bindPort: 55000
```

```json
// usbridge.json
"video_udp_port": 55000
```

FFmpeg шлёт RTP H.264 на 127.0.0.1:55000 → visitor принимает и пересылает в туннель.

---

## Конфигурация на Mac (наше приложение)

Proxy задаётся в `frp_service.go`:
- name: `video_sudp`
- localPort: 55000 (из config.VideoUDPPort)
- secretKey: `sk-video`

GStreamer: `udpsrc port=55000 buffer-size=2097152 caps="application/x-rtp,..." ! rtph264depay ! ...` — RTP H.264.

---

## Порядок запуска

1. Mac: Connect (proxy регистрируется)
2. Bridge: frpc с visitor подключается к frps
3. Mac: GStreamer стартует (udpsrc port=55000)
4. Mac: POST /api/video/start
5. Bridge: FFmpeg шлёт RTP на 127.0.0.1:55000

---

## Чеклист

| Сторона | Компонент | Значение |
|---------|-----------|----------|
| Mac | Proxy name | `video_sudp` |
| Mac | Proxy localPort | 55000 |
| Mac | GStreamer udpsrc RTP | port=55000, rtph264depay |
| Bridge | Visitor serverName | `video_sudp` |
| Bridge | Visitor bindPort | 55000 |
| Bridge | video_udp_port | 55000 |
