#!/bin/zsh
# usage: mac_prep.sh <vhz|0> [margin_us]: sets the streamer's phase lock option, restarts it, starts the renderer
launchctl bootout gui/$(id -u)/lat.render 2>/dev/null
pgrep -f /tmp/echo_mac.py >/dev/null || (nohup python3 /tmp/echo_mac.py >/dev/null 2>&1 &)
caffeinate -u -t 500 >/dev/null 2>&1 &
CONF=$HOME/.config/rustshine/sunshine.conf; LOG="$HOME/Library/Application Support/usbridge-agent/logs/sunshine-stdout.log"
sed -i '' '/^phase_lock_/d' "$CONF"
rm -f /tmp/latrender.log
BASE=$(grep -ac "phase lock: virtual display" "$LOG")
[ "$1" != 0 ] && { echo "phase_lock_source_hz = $1" >> "$CONF"; echo "phase_lock_margin_us = ${2:-3000}" >> "$CONF"; }
kill $(pgrep -f "usbridge-streamer/usbridge-streamer " | head -1); sleep 6
if [ "$1" = 0 ]; then
  /tmp/runjob.sh lat.render /tmp/latrender.log /tmp/latrender -fs=true -swap=0 -fps 240
else
  ( for i in {1..60}; do [ "$(grep -ac 'phase lock: virtual display' "$LOG")" -gt "$BASE" ] && break; sleep 0.5; done; sleep 1.5; /tmp/runjob.sh lat.render /tmp/latrender.log /tmp/latrender -fs=true -swap=0 -last -fps 240 ) >/dev/null 2>&1 &
fi
