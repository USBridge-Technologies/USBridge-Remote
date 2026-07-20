# Logs on Android

## Viewing logs

On Android, app logs are written to **logcat**. Connect the device via USB and run:

```bash
# All app logs (USBridge tag)
adb logcat USBridge:* *:S

# Only USBridge app logs (logrus)
adb logcat -s USBridge

# Clear the buffer before viewing
adb logcat -c && adb logcat USBridge:*
```

## Tags

| Tag | Description |
|-----|----------|
| `USBridge` | Logs from Go (logrus) — API, UI, video |

## Example: debugging video

```bash
adb logcat -c && adb logcat USBridge:* | grep -E "RTP|Frame|video"
```

Look for these in the logs:
- `📨 RTP: first packet received!` — RTP is reaching the pipeline
- `📊 Android: Frame #1 processed` — the decoder produced a frame
- `✅ [VIDEO] Step 7: Frame displayed in UI` — the frame was shown
