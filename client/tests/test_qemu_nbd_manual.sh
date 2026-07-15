#!/usr/bin/env bash
# Ручная проверка экспорта VDI через qemu-nbd и чтение метки тома.
# Запуск: ./scripts/test_qemu_nbd_manual.sh /path/to/image.vdi
#
# 1) Сначала проверяем метку тома (имя диска внутри образа).
# 2) В первом терминале поднимаем qemu-nbd на порту 10810.
# 3) Во втором — подключаемся nbd-client с ПУСТЫМ именем экспорта.
# 4) Проверяем: hexdump -C /dev/nbd0 | head — в конце первых 512 байт должно быть 55 aa (MBR).

set -e
IMAGE="${1:?Укажите путь к VDI: ./test_qemu_nbd_manual.sh /path/to/file.vdi}"
PORT="${2:-10810}"

echo "Образ: $IMAGE"
echo "Порт: $PORT"
echo ""

# Проверка наличия qemu-nbd и qemu-img
if ! command -v qemu-nbd &>/dev/null; then
  echo "Ошибка: qemu-nbd не найден. Установите QEMU (apt install qemu-utils / brew install qemu)"
  exit 1
fi
if ! command -v qemu-img &>/dev/null; then
  echo "Ошибка: qemu-img не найден. Установите QEMU"
  exit 1
fi

# Формат по расширению
ext="${IMAGE##*.}"
ext_lower=$(echo "$ext" | tr '[:upper:]' '[:lower:]')
case "$ext_lower" in
  vdi) FMT=vdi ;;
  vmdk) FMT=vmdk ;;
  qcow2|qcow) FMT=qcow2 ;;
  *) FMT=raw ;;
esac

echo "--- Запуск qemu-nbd ---"
echo "  qemu-nbd -f $FMT -p $PORT -b 127.0.0.1 -r \"$IMAGE\""
echo ""
echo "В другом терминале:"
echo "  sudo nbd-client 127.0.0.1 $PORT /dev/nbd0"
echo "  hexdump -C /dev/nbd0 | head"
echo "  sudo nbd-client -d /dev/nbd0"
echo ""

qemu-nbd -f "$FMT" -p "$PORT" -b 127.0.0.1 -r "$IMAGE"
