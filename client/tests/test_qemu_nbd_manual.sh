#!/usr/bin/env bash
# Manual check of a VDI export via qemu-nbd and reading the volume label.
# Usage: ./scripts/test_qemu_nbd_manual.sh /path/to/image.vdi
#
# 1) First, check the volume label (the disk name inside the image).
# 2) In the first terminal, start qemu-nbd on port 10810.
# 3) In the second — connect nbd-client with an EMPTY export name.
# 4) Check: hexdump -C /dev/nbd0 | head — the end of the first 512 bytes should be 55 aa (MBR).

set -e
IMAGE="${1:?Provide the path to the VDI: ./test_qemu_nbd_manual.sh /path/to/file.vdi}"
PORT="${2:-10810}"

echo "Image: $IMAGE"
echo "Port: $PORT"
echo ""

# Check for qemu-nbd and qemu-img
if ! command -v qemu-nbd &>/dev/null; then
  echo "Error: qemu-nbd not found. Install QEMU (apt install qemu-utils / brew install qemu)"
  exit 1
fi
if ! command -v qemu-img &>/dev/null; then
  echo "Error: qemu-img not found. Install QEMU"
  exit 1
fi

# Format based on extension
ext="${IMAGE##*.}"
ext_lower=$(echo "$ext" | tr '[:upper:]' '[:lower:]')
case "$ext_lower" in
  vdi) FMT=vdi ;;
  vmdk) FMT=vmdk ;;
  qcow2|qcow) FMT=qcow2 ;;
  *) FMT=raw ;;
esac

echo "--- Starting qemu-nbd ---"
echo "  qemu-nbd -f $FMT -p $PORT -b 127.0.0.1 -r \"$IMAGE\""
echo ""
echo "In another terminal:"
echo "  sudo nbd-client 127.0.0.1 $PORT /dev/nbd0"
echo "  hexdump -C /dev/nbd0 | head"
echo "  sudo nbd-client -d /dev/nbd0"
echo ""

qemu-nbd -f "$FMT" -p "$PORT" -b 127.0.0.1 -r "$IMAGE"
