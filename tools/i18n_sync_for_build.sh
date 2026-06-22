#!/usr/bin/env bash
# Sync i18n: merge keys from en.json into all assets locales, refresh embedded English (go:embed), verify parity.
# Intended for macOS / dev (Python 3.7+). Linux/Windows compile-*.sh scripts do not call this.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if ! command -v python3 &>/dev/null; then
  echo "i18n: python3 is required (tools/i18n_merge_from_en.py)" >&2
  exit 1
fi

echo "i18n: merging missing keys from assets/i18n/en.json..."
python3 tools/i18n_merge_from_en.py merge
echo "i18n: copying embedded defaults for go:embed..."
python3 tools/i18n_sync_embedded_defaults.py
echo "i18n: checking locale key parity..."
python3 tools/i18n_merge_from_en.py check
echo "i18n: OK (assets/i18n + internal/i18n/embedded_defaults in sync with en.json structure)."
