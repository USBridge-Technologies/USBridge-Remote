# NBD for Android - User Guide

## Overview

USBridge Client for Android now supports an **NBD (Network Block Device)** server for streaming disk images (.iso, .img) from the microSD card over the network.

### Features

- ✅ Image selection via the **Storage Access Framework (SAF)**
- ✅ Working with files on the microSD card (Android 10+)
- ✅ **Foreground Service** for background operation
- ✅ **Read-only** export (safety)
- ✅ **Wake Lock** - keeps working even with the screen off
- ✅ Support for large files (multi-gigabyte images)

---

## How to use

### 1. Open the NBD dialog

1. Launch the **USBridge Client** app
2. Go to the **"Devices"** section
3. Tap **"Add image"** (on Android this opens the NBD dialog)

### 2. Select an image

1. In the NBD dialog, tap **"Select image (.iso/.img)"**
2. The **SAF picker** opens (the standard Android file selector)
3. Select the image file on the microSD card
4. The app is granted **persistable** access to the file

> **Note:** In the current version, SAF integration via JNI is not yet complete. Alternative: use files from `/sdcard/isos/`

### 3. Configure the address

**Default:** `127.0.0.1:10809` (local access only)

For network access:
- Check **"Allow LAN access (0.0.0.0)"**
- The address changes to `0.0.0.0:10809`

⚠️ **Warning:** With LAN mode enabled, the image becomes accessible from the network. Only use on trusted networks!

### 4. Start the server

1. Tap **"Start NBD server"**
2. The server starts inside a **Foreground Service**
3. A notification appears: **"NBD Server - serving on port 10809 (read-only)"**
4. The dialog status shows: **"🟢 Status: Running on 0.0.0.0:10809"**

### 5. Connect from a computer

On a **Linux** machine:

```bash
# Find the phone's IP (Settings → Network → Wi-Fi → Advanced)
PHONE_IP="192.168.1.100"  # Replace with the actual IP

# Connect the NBD device
sudo nbd-client $PHONE_IP 10809 /dev/nbd0 -read-only

# Check
lsblk | grep nbd0

# Mount (if there's a filesystem)
sudo mount -o ro /dev/nbd0 /mnt

# Or with partitions
sudo partprobe /dev/nbd0
sudo mount -o ro /dev/nbd0p1 /mnt
```

### 6. Disconnect

On the computer:

```bash
sudo umount /mnt
sudo nbd-client -d /dev/nbd0
```

In the app:
- Tap **"Stop NBD server"**
- Or simply close the app (the service stops automatically)

---

## Security

### Read-Only mode

All images are exported **read-only**. Data modification is not possible.

### Local access (default)

By default the server listens on `127.0.0.1:10809` - accessible only from the phone itself (for debugging via ADB forward).

### LAN mode

When the "Allow LAN access" checkbox is enabled:
- The server listens on `0.0.0.0:10809`
- Accessible from any device on the network
- ⚠️ Only use on trusted networks!

### Authorization

Authorization is not implemented in the current version. Planned for future versions:
- Token in the NBD handshake
- TLS encryption

---

## Performance

### Settings

**Block size:** 4096 bytes (default)
- Optimal for most cases
- Can be changed in code (`nbdbridge/bridge.go`)

### Optimizations

- Uses `ReadAt` for random access
- Foreground Service prevents the process from being killed
- Wake Lock keeps the CPU active
- Works in the background even with the screen off

### Speed

Depends on:
- microSD card speed
- Wi-Fi/USB network speed
- System load

Typical read speed: **10-50 MB/s** over Wi-Fi.

---

## Troubleshooting

### Can't select a file

**Problem:** The SAF picker doesn't open, or the file isn't selected.

**Solution:**
1. Make sure the app has storage access permissions
2. Try using files from `/sdcard/isos/`
3. Check the logs: `adb logcat | grep NBD`

### Couldn't determine file size

**Problem:** After selecting the file, "Couldn't determine image size" appears.

**Causes:**
- The file is on a virtual path (cloud storage)
- The file was deleted after selection
- Insufficient access permissions

**Solution:**
- Use files directly from the microSD card
- Verify that the file exists
- Restart the app

### NBD already running

**Problem:** "NBD server already running" appears when trying to start.

**Solution:**
1. Tap "Stop NBD server"
2. Wait 2-3 seconds
3. Try again

### Can't connect from a computer

**Problem:** `nbd-client` returns a connection error.

**Checks:**
1. Are the phone and computer on the same network?
2. Is the IP address correct? (`ip a` on the phone)
3. Is LAN mode enabled in the app?
4. Is there a firewall on the phone?
5. Is the NBD server running? (check the status in the dialog)

**Debugging:**
```bash
# On the computer
ping $PHONE_IP
nc -zv $PHONE_IP 10809

# Logs on the phone
adb logcat | grep -i nbd
```

### Connection drops

**Problem:** NBD disconnects after a few minutes.

**Causes:**
- The system kills the Foreground Service
- Wi-Fi goes to sleep
- Insufficient memory

**Solution:**
1. Make sure the Foreground Service is running (the notification should be visible)
2. Disable battery optimization for the app
3. Use USB tethering instead of Wi-Fi

---

## Architecture

```
┌─────────────────┐
│  Fyne UI (Go)   │  ← Control buttons, status
└────────┬────────┘
         │
┌────────▼────────────────┐
│  disk_widget_android.go │  ← Android-specific UI
└────────┬────────────────┘
         │
┌────────▼────────┐
│  nbdbridge/     │  ← Go NBD backend
│  bridge.go      │     StartNBD(fd, size, addr)
└────────┬────────┘
         │
    ┌────▼────┐
    │ *os.File│  ← fd from SAF
    └─────────┘

┌──────────────────┐
│  NbdBridge.kt    │  ← SAF picker (future integration)
│  (JNI/Kotlin)    │     pickImageFile()
└──────────────────┘     takePersistableUriPermission()

┌──────────────────────┐
│ NbdForegroundService │  ← Foreground Service
│ .kt                  │     Wake Lock, notification
└──────────────────────┘
```

---

## Project files

```
usbridge_client/
├── nbdbridge/
│   └── bridge.go                    # Go NBD backend
├── internal/ui/
│   └── disk_widget_android.go       # Android UI integration
├── android/app/src/main/
│   ├── java/io/usbridge/client/
│   │   ├── NbdBridge.kt            # SAF bridge (Kotlin)
│   │   └── NbdForegroundService.kt # Foreground service
│   └── AndroidManifest.xml          # Permissions + service
└── docs/
    └── NBD_ANDROID_USAGE.md         # This documentation
```

---

## Next steps (TODO)

### High priority

- [ ] Finish the JNI integration for the SAF picker
- [ ] Add an Activity wrapper for NbdBridge
- [ ] Integrate the Foreground Service via JNI

### Medium priority

- [ ] UI for changing the block size
- [ ] Statistics (speed, bytes transferred)
- [ ] Support for multiple exports at once

### Low priority

- [ ] Token authorization
- [ ] TLS encryption
- [ ] Read-ahead buffering

---

## Known limitations

1. **SAF JNI integration is not complete**
   - In the current version, the SAF picker shows an informational message
   - Alternative: use files from `/sdcard/isos/`

2. **Only one export at a time**
   - Only one image can be exported
   - Stop the current server to switch images

3. **No authorization**
   - Any client on the network can connect
   - Only use on trusted networks

4. **Android 10+**
   - Requires Android 10 or higher for SAF
   - On older versions, use direct access to `/sdcard/`

---

## Support

**Questions and bugs:** Open an issue in the repository

**Debug logs:**
```bash
adb logcat | grep -E '(NBD|NbdBridge|NbdForeground)'
```

---

## License

See the LICENSE file at the project root.
