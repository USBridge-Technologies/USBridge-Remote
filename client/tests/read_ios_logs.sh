#!/bin/bash
cd "$(dirname "$0")/.."

DEVICE_ID="68a641ed5b9d24a8d79a4e0cd188ebff199276ce"
APP_BUNDLE=$(find cmd -maxdepth 1 -name "*.app" 2>/dev/null | head -1)

if [ -z "$APP_BUNDLE" ]; then
    APP_BUNDLE=$(find . -maxdepth 1 -name "*.app" 2>/dev/null | head -1)
fi

echo "Launching app on device and collecting logs..."
# Using --noinstall to skip re-installation and just launch it.
ios-deploy --bundle "$APP_BUNDLE" --id "$DEVICE_ID" --no-wifi --justlaunch --debug --noninteractive --noinstall > ios_startup.log 2>&1 &
APP_PID=$!

echo "Waiting 15 seconds for app to start and emit logs..."
sleep 15

echo "Stopping debugger..."
kill -9 $APP_PID 2>/dev/null
wait $APP_PID 2>/dev/null || true

echo "--- iOS STARTUP LOGS ---"
cat ios_startup.log
echo "------------------------"
