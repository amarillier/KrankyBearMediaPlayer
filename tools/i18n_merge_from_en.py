#!/usr/bin/env python3
"""
Keep locale JSON files aligned with en.json (same keys, same nesting).

Why: Chat/LLM "convert this JSON to Portuguese" often truncates or drops keys.
     Line count is meaningless; compare leaf key sets instead.

Usage:
  python3 tools/i18n_merge_from_en.py check          # report missing/extra keys vs en (exit 1 if any missing)
  python3 tools/i18n_merge_from_en.py merge          # add missing keys from en (preserve existing values)

Primary hook: ./tools/i18n_sync_for_build.sh from macOS (compile-mac.sh). That merges keys,
copies en.json into internal/i18n/embedded_defaults, and checks parity (Python 3.7+).
Linux/Windows compile scripts in this repo do not invoke it so older Python on build VMs is OK.
"""
from __future__ import annotations

import copy
import json
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
I18N = REPO / "assets" / "i18n"


def deep_merge_missing(dst: dict, src: dict) -> None:
    """Add keys from src that dst lacks; recurse into dicts. Never overwrite dst leaves."""
    for k, v in src.items():
        if isinstance(k, str) and k.startswith("_"):
            continue
        if k not in dst:
            dst[k] = copy.deepcopy(v)
        elif isinstance(v, dict) and isinstance(dst[k], dict):
            deep_merge_missing(dst[k], v)


def leaf_keys(obj: dict, prefix: str = "") -> set[str]:
    out: set[str] = set()
    for k, v in obj.items():
        if isinstance(k, str) and k.startswith("_"):
            continue
        p = f"{prefix}.{k}" if prefix else k
        if isinstance(v, dict):
            out |= leaf_keys(v, p)
        else:
            out.add(p)
    return out


def main() -> None:
    if len(sys.argv) < 2 or sys.argv[1] not in ("check", "merge"):
        print(__doc__.strip())
        sys.exit(2)

    mode = sys.argv[1]
    en_path = I18N / "en.json"
    with open(en_path, encoding="utf-8") as f:
        en = json.load(f)
    en_keys = leaf_keys(en)

    if mode == "check":
        bad = False
        for path in sorted(I18N.glob("*.json")):
            if path.name == "en.json":
                continue
            with open(path, encoding="utf-8") as f:
                loc = json.load(f)
            loc_keys = leaf_keys(loc)
            missing = sorted(en_keys - loc_keys)
            extra = sorted(loc_keys - en_keys)
            print(f"{path.name}: keys={len(loc_keys)} missing={len(missing)} extra={len(extra)}")
            if missing:
                bad = True
                print("  missing sample:", missing[:15])
            if extra:
                print("  extra sample:", extra[:15])
        sys.exit(1 if bad else 0)

    for path in sorted(I18N.glob("*.json")):
        if path.name == "en.json":
            continue
        with open(path, encoding="utf-8") as f:
            loc = json.load(f)
        loc_keys = leaf_keys(loc)
        extra = sorted(loc_keys - en_keys)
        if extra:
            print(f"warning: {path.name} has {len(extra)} keys not in en.json (left as-is)", file=sys.stderr)
        deep_merge_missing(loc, en)
        with open(path, "w", encoding="utf-8") as f:
            json.dump(loc, f, ensure_ascii=False, indent=2)
            f.write("\n")
        print(f"merged missing keys into {path.name}")


if __name__ == "__main__":
    main()
