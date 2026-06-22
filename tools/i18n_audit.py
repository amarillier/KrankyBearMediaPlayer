#!/usr/bin/env python3
"""
Report i18n leaf strings that still match English (likely untranslated).

Compares assets/i18n/<locale>.json to en.json. Ignores keys whose value is
identical in every locale (e.g. proper nouns) only when you filter by prefix.

Usage:
  python3 tools/i18n_audit.py              # all locales, all keys
  python3 tools/i18n_audit.py --prefix eq
  python3 tools/i18n_audit.py --locale af
"""
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path


def flatten(obj: dict, prefix: str = "") -> dict[str, str]:
    out: dict[str, str] = {}
    for k, v in obj.items():
        if isinstance(k, str) and k.startswith("_"):
            continue
        p = f"{prefix}.{k}" if prefix else k
        if isinstance(v, dict):
            out.update(flatten(v, p))
        else:
            out[p] = str(v) if v is not None else ""
    return out


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--prefix",
        default="",
        help="Only keys starting with this prefix (e.g. eq, report, tag)",
    )
    parser.add_argument(
        "--locale",
        default="",
        help="Only report this locale code (e.g. af). Default: all non-en locales.",
    )
    args = parser.parse_args()

    repo = Path(__file__).resolve().parent.parent
    i18n_dir = repo / "assets" / "i18n"
    if not i18n_dir.is_dir():
        print(f"i18n_audit: missing directory {i18n_dir}", file=sys.stderr)
        return 1

    en_path = i18n_dir / "en.json"
    if not en_path.is_file():
        print(f"i18n_audit: missing {en_path}", file=sys.stderr)
        return 1

    with open(en_path, encoding="utf-8") as f:
        en_flat = flatten(json.load(f))

    pref = args.prefix.strip()
    if pref:
        en_flat = {k: v for k, v in en_flat.items() if k == pref or k.startswith(pref + ".")}

    any_issue = False
    for path in sorted(i18n_dir.glob("*.json")):
        code = path.stem
        if code == "en":
            continue
        if args.locale and code != args.locale:
            continue
        with open(path, encoding="utf-8") as f:
            loc_flat = flatten(json.load(f))
        same: list[str] = []
        for key, en_val in sorted(en_flat.items()):
            if key not in loc_flat:
                continue
            if loc_flat[key] == en_val:
                same.append(key)
        print(f"{code}: {len(same)} strings identical to en (of {len(en_flat)} compared en keys)")
        for k in same[:80]:
            print(f"  {k}")
        if len(same) > 80:
            print(f"  ... and {len(same) - 80} more")
        if same:
            any_issue = True

    return 1 if any_issue else 0


if __name__ == "__main__":
    raise SystemExit(main())
