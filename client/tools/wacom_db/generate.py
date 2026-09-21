#!/usr/bin/env python3
"""Builds internal/usbpass/wacom_db/descriptors.json from a clone of
https://github.com/linuxwacom/wacom-hid-descriptors (real HID report descriptors
dumped from real tablets).

    python tools/wacom_db/generate.py <path to the clone> internal/usbpass/wacom_db/descriptors.json

For every USB Wacom product id the most common set of (interface number, report
descriptor) among the dumps is kept. The interface number comes from the sysfs
path in the dump's udevadm file.
"""
import collections
import json
import re
import subprocess
import sys

repo, out = sys.argv[1], sys.argv[2]


def git(*a):
    return subprocess.run(["git", "-C", repo, *a], capture_output=True).stdout


names = git("ls-tree", "-r", "-z", "--name-only", "HEAD").decode("utf-8", "replace").split("\0")
hid = re.compile(r"^(?P<model>[^/]+)/(?P<dump>[^/]+)/0003:056A:(?P<pid>[0-9A-F]{4})\.(?P<inst>[0-9A-F]{4})\.hid\.bin$")
udev = re.compile(r"^(?P<model>[^/]+)/(?P<dump>[^/]+)/udevadm_0003:056A:(?P<pid>[0-9A-F]{4})\.(?P<inst>[0-9A-F]{4})\.txt$")
ifnum = re.compile(r":1\.(\d+)/0003:056A:[0-9A-F]{4}\.[0-9A-F]{4}")

hids, udevs = {}, {}
for n in names:
    m = hid.match(n)
    if m:
        hids[(m["model"], m["dump"], m["pid"], m["inst"])] = n
    m = udev.match(n)
    if m:
        udevs[(m["model"], m["dump"], m["pid"], m["inst"])] = n

# (model, dump, pid) -> {interface number: descriptor}
dumps = collections.defaultdict(dict)
for key, path in hids.items():
    desc = git("show", "HEAD:" + path)
    if len(desc) < 20:
        continue
    u = udevs.get(key)
    if not u:
        continue
    m = ifnum.search(git("show", "HEAD:" + u).decode("utf-8", "replace"))
    if not m:
        continue
    dumps[key[:3]].setdefault(int(m.group(1)), desc)

by_pid = collections.defaultdict(list)
for (model, dump, pid), ifs in dumps.items():
    by_pid[pid].append((model, tuple(sorted(ifs.items()))))

db = {}
for pid, lst in sorted(by_pid.items()):
    common = collections.Counter(sig for _, sig in lst).most_common(1)[0][0]
    model = next(m for m, sig in lst if sig == common)
    db[pid] = {"name": model, "interfaces": [{"number": n, "report_desc": d.hex()} for n, d in common]}

json.dump(db, open(out, "w"), indent=0, sort_keys=True)
multi = sum(1 for v in db.values() if len(v["interfaces"]) > 1)
print(f"{len(db)} tablets ({multi} with several HID interfaces)")
