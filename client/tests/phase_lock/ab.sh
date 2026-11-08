#!/bin/bash
# usage: ab.sh tag render seconds ab vhz [aim]
P='sshpass -e'; D=${MAC_SSH:?user@mac}; # the password comes from SSHPASS
 tag=$1; r=$2; sec=$3; ab=$4; vhz=$5; aim=${6:-5000}; margin=${7:-1500}
$P ssh $D "/tmp/mac_prep.sh $vhz $margin"
[ "$vhz" = 0 ] && { W=1920; H=1200; settle=3; } || { W=1920; H=1080; settle=8; }
rm -f $tag.clock; ( while :; do python3 clock.py >> $tag.clock; sleep 4; done ) & CL=$!
sleep 3
ABF=""; [ "$ab" != 0 ] && ABF="-phase-ab $ab"
USBRIDGE_PHASE_AIM_US=$aim timeout $((sec+60)) ./streambench -host ${MAC_HOST:?mac address} -codec h265 -width $W -height $H -fps 60 -bitrate 20000 -seconds $sec -settle $settle -render $r $ABF -phase-csv $tag.csv -label mac 2>$tag.err | grep '^{' | tail -1 | python3 -c "import json,sys;m=json.loads(sys.stdin.read() or '{}');print('fps',round(m.get('fps',0),1),'host',round(m.get('host_ms_avg',0),1),'lost',m.get('lost_frames'),'hitches',m.get('hitches'))"
sleep 2; kill $CL
echo "### $tag renderer=$r vhz=$vhz aim=$aim margin=$margin clock range $(awk '{print $2/1000}' $tag.clock | sort -n | sed -n '1p;$p' | tr '\n' ' ') ms"; python3 analyze.py $tag.csv $tag.clock 4
$P ssh $D 'grep -a "phase lock" "$HOME/Library/Application Support/usbridge-agent/logs/sunshine-stdout.log" | tail -1 | sed -E "s/\x1b\[[0-9;]*m//g" | cut -c28-180; L="$HOME/Library/Application Support/usbridge-agent/logs/sunshine-stdout.log"; grep -a "phase test: virtual" "$L" | tail -1 | sed -E "s/\x1b\[[0-9;]*m//g" | cut -c28-150; grep -a "phase lock stats" "$L" | tail -14 | sed -E "s/.*taken: ([0-9]+), skipped: ([0-9]+).*/\1 \2/" | awk "NR==1{a=\$1+\$2} END{print \"real pictures/s from the source:\", (\$1+\$2-a)/((NR-1)*5)}"; launchctl bootout gui/$(id -u)/lat.render 2>/dev/null'
