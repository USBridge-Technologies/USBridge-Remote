# Testing video on Android

An automated build-deploy-verify loop is used to ensure stable Moonlight video streaming on Android.

## Automated test loop

The `scripts/test_android_video.sh` script was created, which performs the following steps:
1.  **Forced rebuild:** Uses `FORCE_FYNE=1` to guarantee that all Go and C code changes make it into the new build.
2.  **Clean install:** Removes the old app version from the device (`adb uninstall`) and installs the new one.
3.  **Automatic connect:** Launches the app via an Android DeepLink with the `immediate=true` parameter, which makes the app skip confirmation dialogs and start connecting instantly.
4.  **Log monitoring:** Runs `adb logcat` for 10 seconds, filtered by key tags (`Moonlight`, `VideoGL`, `AMediaCodec`), to show the connection result and the first video frames.

### How to run the test
```bash
# Run the full cycle (build + deploy + 10s of logs)
./scripts/test_android_video.sh
```

## Strategies for ensuring video works

### 1. Working around Tailscale (userspace) limitations
On Android, Tailscale runs in userspace mode (`tsnet`). Moonlight's native C code (the `Limelight` library) cannot use the Go library's network stack and instead tries to work through system sockets, which cannot see `100.x.x.x`.
- **Solution:** We implemented discovery of the peer's "direct" LAN IP (`192.168.x.x`) via the Tailscale API. If the phone and server are on the same network, Moonlight connects directly over the local IP, bypassing the VPN tunnel for the video stream itself.

### 2. JNI and UI-thread optimization
Frequent JNI calls from different threads can block Android's main UI thread, causing interface freezes.
- **Solution:** JavaVM initialization and caching of the required Java classes (`VideoSurfaceBridge`) happens **exactly once**, on the first video start. We use `sync.Once` in Go to prevent repeated blocking `driver.RunNative` calls.

### 3. Deep tracing
`ALOGI` and `ALOGE` macros were added to `internal/service/moonlight_cgo_android.go` for debugging native code.
- They print logs directly to the Android system `logcat`.
- All stages are logged: `do_li_start`, `AMediaCodec` initialization, `ANativeWindow` creation, and `EGL` context binding.

## Expected result in the logs
On a successful video start, the following markers should appear in the logs:
1. `🎬 [Moonlight/Android] JNI Initialized (VER: V4_...)` — JNI bound successfully.
2. `Moonlight/CGO: do_li_start: STARTING addr=192.168.1.125 ...` — native connection starting.
3. `Moonlight/CGO: AMediaCodec started successfully` — hardware decoder initialized.
4. `🎬 [Moonlight/HW/Android] ✅ first RGBA frame — 640x480` — first frame successfully decoded and read from the GPU into Go.
