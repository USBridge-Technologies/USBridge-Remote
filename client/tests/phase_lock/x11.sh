#!/bin/bash
# X11 overlay path as the GUI uses it: lag growth per renderer, Net Graph on/off
P='sshpass -e'; D=${MAC_SSH:?user@mac}
$P ssh $D "/tmp/mac_prep.sh 0"
for mode in "1920 1080 60" "2560 1440 60" "2560 1440 120"; do set -- $mode; for r in vk gl; do for ng in false true; do
 T="$1x$2@$3 $r ng=$ng"; rm -f m.json; timeout 30 ./streambench -host ${MAC_HOST:?mac address} -codec h265 -width $1 -height $2 -fps $3 -bitrate 20000 -seconds 10 -settle 2 -render $r -netgraph=$ng -label mac -dump m.json 2>m.err | grep '^{' | python3 an2.py m.json "$T" 2>&1 | tail -1; [ -f m.json ] || echo "$T FAILED"
done; done; done
echo "## forced RGBA path (slow client) with the guard, then without"
for g in 200 0; do rm -f m.json; USBRIDGE_FORCE_RGBA=1 USBRIDGE_BACKLOG_GUARD_MS=$g timeout 30 ./streambench -host ${MAC_HOST:?mac address} -codec h265 -width 2560 -height 1440 -fps 60 -bitrate 20000 -seconds 20 -settle 2 -render gl -label mac -dump m.json 2>m.err | grep '^{' | python3 an2.py m.json "guard=${g}ms 1440p60 gl rgba"; done
