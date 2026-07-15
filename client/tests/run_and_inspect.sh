#!/bin/bash
cd "$(dirname "$0")/.."

echo "Building application..."
go build -o usbridge-test-app ./cmd

echo "Running application in background..."
./usbridge-test-app > startup_output.log 2>&1 &
APP_PID=$!

echo "Waiting for 3 seconds to let it initialize..."
sleep 3

echo "Killing application..."
kill $APP_PID
wait $APP_PID 2>/dev/null

echo "--- STARTUP CONTENT / LOGS ---"
cat startup_output.log
echo "------------------------------"
