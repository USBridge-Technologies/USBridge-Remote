from __future__ import annotations

import sys
from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    if new in text:
        return text
    if old not in text:
        raise RuntimeError(f"patch target not found for {label}")
    return text.replace(old, new, 1)


def patch_file(path: Path, transform) -> None:
    if not path.exists():
        return
    original = path.read_text(encoding="utf-8")
    updated = transform(original)
    if updated != original:
        with path.open("w", encoding="utf-8", newline="\n") as fh:
            fh.write(updated)


def ensure_after(text: str, anchor: str, addition: str, label: str) -> str:
    if addition in text:
        return text
    if anchor not in text:
        raise RuntimeError(f"patch anchor not found for {label}")
    return text.replace(anchor, anchor + addition, 1)


def patch_gstreamer_root(root: Path) -> None:
    meson_build = root / "meson.build"
    patch_file(
        meson_build,
        lambda text: replace_once(
            text,
            "if build_system == 'windows'\n"
            "  subproject('win-flex-bison-binaries')\n"
            "  subproject('win-nasm')\n"
            "elif build_system == 'darwin'\n",
            "if build_system == 'windows'\n"
            "  subproject('win-flex-bison-binaries')\n"
            "  local_nasm = find_program('nasm', required: false)\n"
            "  if not local_nasm.found()\n"
            "    subproject('win-nasm')\n"
            "  endif\n"
            "elif build_system == 'darwin'\n",
            "gstreamer meson.build",
        ),
    )

    ffmpeg_meson = root / "subprojects" / "FFmpeg" / "meson.build"
    patch_file(
        ffmpeg_meson,
        lambda text: replace_once(
            text,
            "conf.set10('nanosleep', cc.has_function('nanosleep'), prefix: '#include <time.h>')",
            "conf.set10('nanosleep', cc.has_function('nanosleep', prefix: '#include <time.h>'))",
            "FFmpeg nanosleep",
        ),
    )

    ugly_meson = root / "subprojects" / "gst-plugins-ugly" / "meson.build"

    def transform_ugly_meson(text: str) -> str:
        if (
            "if not meson.is_cross_build() and build_machine.system() != 'windows'\n"
            "  subdir('docs')\n"
            "endif\n" in text
        ):
            return text
        return text.replace(
            "subdir('docs')\n",
            "if not meson.is_cross_build() and build_machine.system() != 'windows'\n"
            "  subdir('docs')\n"
            "endif\n",
            1,
        )

    patch_file(ugly_meson, transform_ugly_meson)

    gdbus_utils = root / "subprojects" / "glib" / "gio" / "gdbus-2.0" / "codegen" / "utils.py"

    def transform_gdbus_utils(text: str) -> str:
        text = replace_once(
            text,
            "import distutils.version\nimport os\nimport sys\n",
            "import os\nimport re\nimport sys\n",
            "gdbus imports",
        )
        return replace_once(
            text,
            "def version_cmp_key(key):\n"
            "    # If the 'since' version is 'UNRELEASED', compare higher than anything else\n"
            "    # If it is empty put a 0 in its place as this will\n"
            "    # allow LooseVersion to work and will always compare lower.\n"
            "    if key[0] == \"UNRELEASED\":\n"
            "        v = \"9999\"\n"
            "    elif key[0]:\n"
            "        v = str(key[0])\n"
            "    else:\n"
            "        v = \"0\"\n"
            "    return (distutils.version.LooseVersion(v), key[1])\n",
            "def version_cmp_key(key):\n"
            "    # If the 'since' version is 'UNRELEASED', compare higher than anything else\n"
            "    # If it is empty put a 0 in its place as this will\n"
            "    # allow version parsing to work and will always compare lower.\n"
            "    if key[0] == \"UNRELEASED\":\n"
            "        v = \"9999\"\n"
            "    elif key[0]:\n"
            "        v = str(key[0])\n"
            "    else:\n"
            "        v = \"0\"\n"
            "    parts = []\n"
            "    for part in re.split(r\"([0-9]+)\", v):\n"
            "        if not part:\n"
            "            continue\n"
            "        if part.isdigit():\n"
            "            parts.append((0, int(part)))\n"
            "        else:\n"
            "            parts.append((1, part))\n"
            "    return (tuple(parts), key[1])\n",
            "gdbus version_cmp_key",
        )

    patch_file(gdbus_utils, transform_gdbus_utils)

    sysprof_util = root / "subprojects" / "sysprof" / "src" / "libsysprof-capture" / "sysprof-capture-util-private.h"

    def transform_sysprof_util(text: str) -> str:
        if "#include <errno.h>\n" not in text:
            text = text.replace("#include <stdlib.h>\n", "#include <errno.h>\n#include <stdlib.h>\n", 1)
        if "#include <stdint.h>\n" not in text:
            text = text.replace("#include <stdlib.h>\n", "#include <stdint.h>\n#include <stdlib.h>\n", 1)

        fallback = (
            "#if !defined(HAVE_REALLOCARRAY) && !defined(reallocarray)\n"
            "static inline void *\n"
            "sysprof_reallocarray (void   *ptr,\n"
            "                      size_t  nmemb,\n"
            "                      size_t  size)\n"
            "{\n"
            "  if (size != 0 && nmemb > SIZE_MAX / size)\n"
            "    {\n"
            "      errno = ENOMEM;\n"
            "      return NULL;\n"
            "    }\n\n"
            "  return realloc (ptr, nmemb * size);\n"
            "}\n"
            "# define reallocarray sysprof_reallocarray\n"
            "#endif\n\n"
        )
        return ensure_after(
            text,
            "static inline void *\n"
            "sysprof_malloc0 (size_t size)\n"
            "{\n"
            "  void *ptr = malloc (size);\n"
            "  if (ptr == NULL)\n"
            "    return NULL;\n"
            "  memset (ptr, 0, size);\n"
            "  return ptr;\n"
            "}\n\n",
            fallback,
            "sysprof reallocarray fallback",
        )

    patch_file(sysprof_util, transform_sysprof_util)

    sysprof_writer_cat = root / "subprojects" / "sysprof" / "src" / "libsysprof-capture" / "sysprof-capture-writer-cat.c"
    patch_file(
        sysprof_writer_cat,
        lambda text: ensure_after(
            text,
            '#include "sysprof-capture.h"\n',
            '#include "sysprof-capture-util-private.h"\n',
            "sysprof writer-cat include",
        ),
    )

    sysprof_collector = root / "subprojects" / "sysprof" / "src" / "libsysprof-capture" / "sysprof-collector.c"

    def transform_sysprof_collector(text: str) -> str:
        text = text.replace(
            "  fcntl_flags = fcntl (peer_fd, F_GETFL);\n",
            "  fcntl_flags = fcntl (fd, F_GETFL);\n",
        )
        return text.replace(
            "  if (fcntl (peer_fd, F_SETFL, fcntl_flags) == -1)\n",
            "  if (fcntl (fd, F_SETFL, fcntl_flags) == -1)\n",
        )

    patch_file(sysprof_collector, transform_sysprof_collector)

    sysprof_macros = root / "subprojects" / "sysprof" / "src" / "libsysprof-capture" / "sysprof-macros.h"
    patch_file(
        sysprof_macros,
        lambda text: replace_once(
            text,
            "#ifdef __cpp_static_assert\n"
            "# define SYSPROF_STATIC_ASSERT(expr, msg) static_assert(expr, msg)\n"
            "#elif SYSPROF_GNUC_CHECK_VERSION(4, 6)\n"
            "# define SYSPROF_STATIC_ASSERT(expr, msg) _Static_assert(expr, msg)\n"
            "#else\n"
            "# define SYSPROF_STATIC_ASSERT(expr, msg) char __static_assert_##__COUNTER__ [(expr) ? 0 : -1];\n"
            "#endif\n",
            "#ifdef __cpp_static_assert\n"
            "# define SYSPROF_STATIC_ASSERT(expr, msg) static_assert(expr, msg)\n"
            "#elif defined(__STDC_VERSION__) && (__STDC_VERSION__ >= 201112L)\n"
            "# define SYSPROF_STATIC_ASSERT(expr, msg) _Static_assert(expr, msg)\n"
            "#elif defined(__clang__)\n"
            "# define SYSPROF_STATIC_ASSERT(expr, msg) _Static_assert(expr, msg)\n"
            "#elif SYSPROF_GNUC_CHECK_VERSION(4, 6)\n"
            "# define SYSPROF_STATIC_ASSERT(expr, msg) _Static_assert(expr, msg)\n"
            "#else\n"
            "# define SYSPROF_STATIC_ASSERT(expr, msg) char __static_assert_##__COUNTER__ [(expr) ? 0 : -1];\n"
            "#endif\n",
            "sysprof static assert",
        ),
    )


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: patch_gstreamer_android_checkout.py <gstreamer-dir>", file=sys.stderr)
        return 2

    root = Path(sys.argv[1])
    if not root.is_dir():
        print(f"gstreamer dir not found: {root}", file=sys.stderr)
        return 1

    patch_gstreamer_root(root)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
