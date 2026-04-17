#!/usr/bin/env python3
import struct
import sys
from pathlib import Path


PNG_SIGNATURE = b"\x89PNG\r\n\x1a\n"


def png_size(data: bytes) -> tuple[int, int]:
    if not data.startswith(PNG_SIGNATURE):
        raise ValueError("not a PNG file")
    if len(data) < 24:
        raise ValueError("PNG file too short")
    width = struct.unpack(">I", data[16:20])[0]
    height = struct.unpack(">I", data[20:24])[0]
    return width, height


def icon_dim_byte(value: int) -> int:
    return 0 if value >= 256 else value


def build_ico(paths: list[Path], out_path: Path) -> None:
    images: list[tuple[int, int, bytes]] = []
    for path in paths:
        data = path.read_bytes()
        width, height = png_size(data)
        images.append((width, height, data))

    images.sort(key=lambda item: item[0] * item[1])

    header = struct.pack("<HHH", 0, 1, len(images))
    entries = []
    offset = 6 + len(images) * 16

    for width, height, data in images:
        entries.append(
            struct.pack(
                "<BBBBHHII",
                icon_dim_byte(width),
                icon_dim_byte(height),
                0,
                0,
                1,
                32,
                len(data),
                offset,
            )
        )
        offset += len(data)

    out_path.write_bytes(header + b"".join(entries) + b"".join(data for _, _, data in images))


def main() -> int:
    if len(sys.argv) < 3:
        print("usage: generate_windows_ico.py OUTPUT.ico INPUT1.png [INPUT2.png ...]", file=sys.stderr)
        return 1

    out_path = Path(sys.argv[1])
    inputs = [Path(arg) for arg in sys.argv[2:]]
    build_ico(inputs, out_path)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
