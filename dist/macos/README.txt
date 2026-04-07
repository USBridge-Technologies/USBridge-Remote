USBridgeAgent for macOS
=======================

Run:
  ./scripts/install_macos.sh
  open "$HOME/Applications/USBridgeAgent.app"

Requirements:
  - ffmpeg must be installed and available in PATH
  - Accessibility permission for mouse/keyboard injection
  - Screen Recording permission for screen capture

Notes:
  - For stable macOS permissions, always run the same installed app path
  - Recommended install path: ~/Applications/USBridgeAgent.app
  - Dev builds are left unsigned by default; set USBRIDGE_CODESIGN_IDENTITY to sign explicitly
  - Video capture uses FFmpeg AVFoundation screen devices
  - Snapshot capture uses the built-in screencapture utility
  - The agent remains API-compatible with usbridge_client

Configuration:
  config.yaml next to the .app, or ~/.config/usbridge-agent/

Application log:
  ~/Library/Logs/USBridgeAgent/app.log
  If USBRIDGE_LOG_DIR is set, logs are written there instead.
