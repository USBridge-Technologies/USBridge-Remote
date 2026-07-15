#!/usr/bin/env python3
"""
Screen-content validator for the Android UI stress test (test_android_ui_stress.sh).

Two independent checks, both driven by content the script pulls from the
device itself (no manual screen-reading):

  tab   <screenshot.png> [expected]
        Classifies a screenshot as the connection-manager screen or one of
        the four in-session tabs (Control/Devices/Snapshots/Scripts) from
        pixel content. Fyne renders the whole UI into one opaque GL/Vulkan
        surface, so `uiautomator dump` exposes no text for these — pixel
        sampling is the only way to identify which tab is showing.
        Detection: the in-session tab bar row (y in [190,260] at the
        reference 1080x2400 resolution) renders the active tab's label and
        underline in the app's accent green (~RGB 147,197,114); the
        connection-manager screen has zero matching pixels there. The active
        tab is identified by the x-centroid of the matching pixels.

  leak  <uiautomator_dump.xml> [max_allowed]
        Structural check for the leftover-Vulkan-overlay bug: parses the
        accessibility-tree XML (`adb shell uiautomator dump`, pulled locally)
        and counts top-level `android.view.View` nodes with non-zero bounds.
        Fyne's own content view is always exactly one such node. The
        VulkanOverlayBridge SurfaceView (see android/.../VulkanOverlayBridge.kt)
        is added directly to the Activity's decorView as a *second* sibling
        View — confirmed empirically: dumping while connected shows 2 View
        nodes, dumping after a clean disconnect shows 1. So on the
        connections screen, count > max_allowed (default 1) means the overlay
        was never torn down (destroy() didn't run / raced) — the exact bug
        being hunted. Exits 0 if count <= max_allowed, else 1.

Always prints one result line for grepping from the calling shell script.
"""
import sys
import xml.etree.ElementTree as ET
from PIL import Image

# Reference resolution the tap coordinates / bands below were calibrated at.
REF_W, REF_H = 1080, 2400

TAB_BAND_Y = (190, 260)

# Green-pixel classification thresholds (accent color ~ (147,197,114)).
def _is_accent_green(r, g, b):
    return 100 < r < 190 and 160 < g < 220 and 80 < b < 160 and g > r > b

# x-centroid boundaries (in reference-resolution pixels) separating the four
# tabs, derived from calibration screenshots.
TAB_BOUNDS = [
    ("Control", 0, 206),
    ("Devices", 206, 420),
    ("Snapshots", 420, 690),
    ("Scripts", 690, REF_W),
]

MIN_GREEN_PX_FOR_SESSION = 300


def classify(path):
    im = Image.open(path).convert("RGB")
    w, h = im.size
    sx, sy = w / REF_W, h / REF_H

    y0 = int(TAB_BAND_Y[0] * sy)
    y1 = int(TAB_BAND_Y[1] * sy)

    xs = []
    for y in range(y0, y1):
        row = im.crop((0, y, w, y + 1)).load()
        for x in range(w):
            r, g, b = row[x, 0]
            if _is_accent_green(r, g, b):
                xs.append(x / sx)  # normalize back to reference scale

    n = len(xs)
    if n < MIN_GREEN_PX_FOR_SESSION:
        return "connections", n, None

    cx = sum(xs) / n
    for name, lo, hi in TAB_BOUNDS:
        if lo <= cx < hi:
            return f"session:{name}", n, cx
    return "session:unknown", n, cx


def count_surface_views(xml_path):
    """Count android.view.View nodes with non-zero bounds in a uiautomator dump."""
    tree = ET.parse(xml_path)
    n = 0
    for node in tree.getroot().iter("node"):
        if node.attrib.get("class") == "android.view.View" and node.attrib.get("bounds") != "[0,0][0,0]":
            n += 1
    return n


def cmd_tab(argv):
    if not argv:
        print(__doc__)
        sys.exit(2)
    path = argv[0]
    expected = argv[1] if len(argv) > 1 else None

    label, n, cx = classify(path)
    cx_s = f"{cx:.0f}" if cx is not None else "-"
    print(f"{label} green_px={n} centroid_x={cx_s}")

    if expected is None:
        sys.exit(0)
    expected_label = "connections" if expected.lower() == "connections" else f"session:{expected}"
    sys.exit(0 if label == expected_label else 1)


def cmd_leak(argv):
    if not argv:
        print(__doc__)
        sys.exit(2)
    xml_path = argv[0]
    max_allowed = int(argv[1]) if len(argv) > 1 else 1

    n = count_surface_views(xml_path)
    status = "CLEAN" if n <= max_allowed else "LEAK"
    print(f"{status} view_count={n} max_allowed={max_allowed}")
    sys.exit(0 if n <= max_allowed else 1)


def main():
    if len(sys.argv) < 2:
        print(__doc__)
        sys.exit(2)

    # Back-compat: `android_ui_check.py <png> [expected]` == `tab` subcommand.
    if sys.argv[1] in ("tab", "leak"):
        mode, rest = sys.argv[1], sys.argv[2:]
    elif sys.argv[1].lower().endswith(".xml"):
        mode, rest = "leak", sys.argv[1:]
    else:
        mode, rest = "tab", sys.argv[1:]

    if mode == "tab":
        cmd_tab(rest)
    else:
        cmd_leak(rest)


if __name__ == "__main__":
    main()
