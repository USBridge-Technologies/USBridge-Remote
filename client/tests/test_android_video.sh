#!/bin/bash
set -e

echo "🚀 Starting Automated Android Build & Test cycle..."

# 1. Build the APK (force recompile Go code)
echo "🔨 Building APK (FORCE_FYNE=1)..."
FORCE_FYNE=1 ./scripts/build_android.sh

# 2. Find the APK
APK_PATH="$(ls dist/android/USBridgeClient-Android-*.apk 2>/dev/null | head -1)"
if [ -z "$APK_PATH" ]; then
    echo "❌ APK not found in dist/android/!"
    exit 1
fi
echo "✅ APK found at: $APK_PATH"

# 3. Update the APK (incremental, no uninstall — keeps permissions)
echo "📲 Updating APK on device (incremental)..."
adb install -r "$APK_PATH"

# 4. Clear logs and trigger connection
echo "🧹 Clearing logcat..."
adb logcat -c

echo "🔗 Triggering connection to 100.71.26.121 (Tailscale IP)..."
# Inner single quotes protect & from Android shell interpretation
adb shell "am start -a android.intent.action.VIEW -d 'usbridge://connect?host=100.71.26.121&quic_token=YOUR_QUIC_TOKEN&immediate=true'"

# 5. Monitor logs for video events for 25s
echo "📋 Monitoring logcat for video results (25s timeout)..."
echo "--- LOGS START ---"
adb logcat -v time | grep -E "USBridge|Moonlight/CGO|VideoGL|VideoSurfaceBridge|AMediaCodec" &
LOGCAT_PID=$!
sleep 25
kill $LOGCAT_PID 2>/dev/null || true
echo "--- LOGS END ---"
