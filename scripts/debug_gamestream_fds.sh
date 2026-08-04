#!/bin/bash
# Narrow, read-only debugging helper: lists what every open file descriptor
# of a running gamestream-server process actually points at (readlink +
# fdinfo), to diagnose an fd leak. Intentionally does nothing else — no
# arbitrary command execution, no writes, so it's safe to grant passwordless
# root for exactly this one command via sudoers rather than via setuid
# (setuid is silently ignored by the kernel for shebang scripts anyway).
#
# Usage: sudo ./scripts/debug_gamestream_fds.sh <pid>
#
# To allow running this without a password prompt, add a narrowly-scoped
# sudoers rule (as root, e.g. via `sudo visudo -f /etc/sudoers.d/gamestream-debug`):
#
#   amir ALL=(root) NOPASSWD: /media/amir/kubuntu_2510/home/amir/Projects/USBridge-Remote/scripts/debug_gamestream_fds.sh
#
# That grants exactly this script, not a general root shell -- revoke by
# deleting that file.

set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
    echo "must run as root (sudo $0 <pid>)" >&2
    exit 1
fi

pid="${1:-}"
if ! [[ "$pid" =~ ^[0-9]+$ ]]; then
    echo "usage: $0 <pid>" >&2
    exit 1
fi

fd_dir="/proc/$pid/fd"
if [ ! -d "$fd_dir" ]; then
    echo "no such process: $pid" >&2
    exit 1
fi

comm="$(cat "/proc/$pid/comm" 2>/dev/null || echo unknown)"
echo "# pid=$pid comm=$comm"

count=0
declare -A by_target_kind

for entry in "$fd_dir"/*; do
    fd="$(basename "$entry")"
    target="$(readlink "$entry" 2>/dev/null || echo '?')"
    flags="$(awk -F': ' '/^flags:/{print $2}' "/proc/$pid/fdinfo/$fd" 2>/dev/null || echo '?')"
    echo "fd=$fd target=$target flags=$flags"

    kind="other"
    case "$target" in
        socket:*) kind="socket" ;;
        pipe:*) kind="pipe" ;;
        anon_inode:*) kind="anon_inode:${target#anon_inode:}" ;;
        /dev/dri/*) kind="dri:${target}" ;;
        /dev/*) kind="dev:${target}" ;;
        *) kind="file" ;;
    esac
    by_target_kind["$kind"]=$(( ${by_target_kind["$kind"]:-0} + 1 ))
    count=$((count + 1))
done

echo "# total fds: $count"
echo "# breakdown by kind:"
for k in "${!by_target_kind[@]}"; do
    printf '#   %-40s %d\n' "$k" "${by_target_kind[$k]}"
done | sort -t' ' -k3 -rn
