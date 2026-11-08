#!/bin/zsh
# usage: mac_prep.sh <vhz|0> ; prepares the streamer's test flag and the renderer
launchctl bootout gui/$(id -u)/lat.render 2>/dev/null
pgrep -f /tmp/echo_mac.py >/dev/null || (nohup python3 /tmp/echo_mac.py >/dev/null 2>&1 &)
caffeinate -u -t 400 >/dev/null 2>&1 &
rm -f /tmp/usbridge-phase-display /tmp/latrender.log
if [ "$1" = 0 ]; then
  rm -f /tmp/usbridge-phase-test
  /tmp/runjob.sh lat.render /tmp/latrender.log /tmp/latrender -fs=true -swap=0 -fps 240
else
  echo "vhz=$1 margin_us=${2:-1500}" > /tmp/usbridge-phase-test
  ( for i in {1..30}; do [ -s /tmp/usbridge-phase-display ] && break; sleep 0.5; done; sleep 1; /tmp/runjob.sh lat.render /tmp/latrender.log /tmp/latrender -fs=true -swap=0 -last -fps 240 ) >/dev/null 2>&1 &
fi
