#!/bin/bash
cd "$(dirname "$0")/.."

DEVICE_ID="68a641ed5b9d24a8d79a4e0cd188ebff199276ce"
APP_BUNDLE=$(find cmd -maxdepth 1 -name "*.app" 2>/dev/null | head -1)
if [ -z "$APP_BUNDLE" ]; then
    APP_BUNDLE=$(find . -maxdepth 1 -name "*.app" 2>/dev/null | head -1)
fi

echo "Starting syslog stream..."
idevicesyslog -u "$DEVICE_ID" > ios_syslog.txt 2>&1 &
SYSLOG_PID=$!

sleep 2
echo "Launching app..."
ios-deploy --id "$DEVICE_ID" --bundle "$APP_BUNDLE" --justlaunch --noinstall --noninteractive

echo "Waiting for crash..."
sleep 5

echo "Stopping syslog..."
kill -9 $SYSLOG_PID 2>/dev/null

echo "--- SYSLOG OUTPUT ---"
# Filter syslog for our process or general crash indicators
grep -iE 'main|usbridge|panic|crash|exception' ios_syslog.txt | tail -n 100
echo "---------------------"
