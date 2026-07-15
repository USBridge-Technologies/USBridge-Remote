# Logs on Android

## Viewing logs

On Android, app logs are written to **logcat**. Connect the device via USB and run:

```bash
# All app logs (USBridge and GStreamer-Static tags)
adb logcat USBridge:* GStreamer-Static:* *:S

# Only USBridge app logs (logrus)
adb logcat -s USBridge

# Clear the buffer before viewing
adb logcat -c && adb logcat USBridge:* GStreamer-Static:*
```

## Tags

| Tag | Description |
|-----|----------|
| `USBridge` | Logs from Go (logrus) — API, UI, video, FRP |
| `GStreamer-Static` | GStreamer logs — pipeline, RTP, decoder |

## Example: debugging video

```bash
adb logcat -c && adb logcat USBridge:* GStreamer-Static:* | grep -E "RTP|Frame|GStreamer|video"
```

Look for these in the logs:
- `📨 RTP: first packet received!` — RTP is reaching the pipeline
- `📊 Android: Frame #1 processed` — the decoder produced a frame
- `✅ [VIDEO] Step 7: Frame displayed in UI` — the frame was shown
