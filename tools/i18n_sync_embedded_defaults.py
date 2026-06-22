#!/usr/bin/env python3
"""
Copy English artifacts shipped via go:embed from assets/i18n (and LICENSE) into internal/i18n/embedded_defaults.

Embedded in the binary:
  - en.json — UI string baseline
  - help_en.txt, about_en.txt, updates_en.txt — long-form English copy
  - license_en.txt — from repo LICENSE (legal source of truth)

All other locales and non-English help/about/updates/license live under assets/i18n/ and install beside the app.

Run after editing assets/i18n/en.json, the bundled *_en.txt files, or LICENSE.
"""
from __future__ import annotations

import shutil
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
SRC = REPO / "assets" / "i18n"
DST = REPO / "internal" / "i18n" / "embedded_defaults"
LICENSE_SRC = REPO / "LICENSE"


def main() -> int:
    DST.mkdir(parents=True, exist_ok=True)
    for name in ("en.json", "help_en.txt", "about_en.txt", "updates_en.txt"):
        sfile = SRC / name
        if not sfile.is_file():
            print(f"i18n: embedded sync requires {sfile}", file=sys.stderr)
            return 1
        shutil.copy2(sfile, DST / name)
        print(f"i18n: embedded_defaults/{name} <- assets/i18n/{name}")

    if not LICENSE_SRC.is_file():
        print(f"i18n: embedded sync requires {LICENSE_SRC}", file=sys.stderr)
        return 1
    shutil.copy2(LICENSE_SRC, DST / "license_en.txt")
    print("i18n: embedded_defaults/license_en.txt <- LICENSE")

    shutil.copy2(LICENSE_SRC, SRC / "license_en.txt")
    print("i18n: assets/i18n/license_en.txt <- LICENSE")

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
