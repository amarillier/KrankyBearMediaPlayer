#!/usr/bin/env python3
"""Merge tools/i18n_staged/<lang>.json into assets/i18n/<lang>.json (deep overwrite).

Use when an external AI returns a partial catalog (only some sections translated):
drop it as tools/i18n_staged/<lang>.json, run this, then i18n_merge_from_en.py to
backfill any still-missing keys with English.
"""
from __future__ import annotations

import json
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
STAGED = REPO / "tools" / "i18n_staged"
ASSETS = REPO / "assets" / "i18n"


def deep_overwrite(dst: dict, src: dict) -> None:
    for k, v in src.items():
        if isinstance(v, dict) and k in dst and isinstance(dst.get(k), dict):
            deep_overwrite(dst[k], v)
        else:
            dst[k] = v


def main() -> None:
    langs = [p.stem for p in sorted(STAGED.glob("*.json"))]
    if not langs:
        print("no tools/i18n_staged/*.json", file=sys.stderr)
        sys.exit(1)
    for lang in langs:
        part_path = STAGED / f"{lang}.json"
        out_path = ASSETS / f"{lang}.json"
        with open(part_path, encoding="utf-8") as f:
            partial = json.load(f)
        with open(out_path, encoding="utf-8") as f:
            full = json.load(f)
        deep_overwrite(full, partial)
        with open(out_path, "w", encoding="utf-8") as f:
            json.dump(full, f, ensure_ascii=False, indent=2)
            f.write("\n")
        print("updated", out_path)


if __name__ == "__main__":
    main()
