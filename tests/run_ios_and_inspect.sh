#!/bin/bash
cd "$(dirname "$0")/.."

echo "Building and deploying to iOS..."
./scripts/deploy_ios.sh

echo "Finding device and app bundle..."
DEVICE_ID=$(xcrun xctrace list devices 2>/dev/null | grep -v "MacBook" | grep -v "Simulator" | grep -v "Offline" | grep -o -E "\([0-9a-f]{40}\)" | head -1 | tr -d '()')
APP_BUNDLE=$(find cmd -maxdepth 1 -name "*.app" 2>/dev/null | head -1)

if [ -z "$APP_BUNDLE" ]; then
    APP_BUNDLE=$(find . -maxdepth 1 -name "*.app" 2>/dev/null | head -1)
fi

if [ -z "$DEVICE_ID" ] || [ -z "$APP_BUNDLE" ]; then
    echo "Device or app bundle not found!"
    exit 1
fi

echo "Launching app on device and collecting logs..."
ios-deploy --bundle "$APP_BUNDLE" --id "$DEVICE_ID" --no-wifi --justlaunch --debug --noninteractive > ios_startup.log 2>&1 &
APP_PID=$!

echo "Waiting 7 seconds for logs..."
sleep 7

echo "Stopping debugger..."
kill -9 $APP_PID 2>/dev/null
wait $APP_PID 2>/dev/null || true

echo "--- iOS STARTUP LOGS ---"
cat ios_startup.log
echo "------------------------"
