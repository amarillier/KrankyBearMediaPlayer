# Contributor language packs

Extra UI languages are loaded from `assets/i18n` at runtime. **English** ships in the binary as **`en.json`** plus long-form defaults (`help_en.txt`, `about_en.txt`, `updates_en.txt`, `license_en.txt` under `internal/i18n/embedded_defaults/`, mirrored into this folder). Dropping files into `assets/i18n` overrides those defaults at runtime. Non-English: add `xx.json` and any optional `*_xx.txt` files below; missing pieces fall back to English.

Ideal contributors are native or fluent speakers who create or review translations (including AI-assisted drafts) for accuracy and tone.

> **Maintainer note:** `hb.json` (Hillbilly — a playful, internal-only test locale) and `af.json` (Afrikaans) are the maintainer's easy-to-read smoke-test packs while the UI string surface is migrated slice by slice. `hb` is **not** for customer builds.

## What to add

| File | Required | Purpose |
|------|----------|---------|
| `xx.json` | Yes | UI strings (same key structure as `en.json`). |
| `help_xx.txt` | No | In-app Help window. Falls back to English embedded help. |
| `about_xx.txt` | No | About dialog body (`{{app_name}}`, `{{version}}`, `{{copyright}}`). Falls back to English. |
| `updates_xx.txt` | No | Check for Updates dialog text (`{{version}}`, `{{releases_url}}`). Falls back to English. |
| `license_xx.txt` | No | Licence text shown in the About / License window. Falls back to English (`license_en.txt`). |

Replace `xx` with your **two-letter ISO 639-1** code in lowercase (e.g. Greek: `el.json`, `help_el.txt`, `about_el.txt`, …). Only `xx.json` names matching `^[a-z]{2}\.json$` are loaded as UI catalogs (so `en.json` itself is skipped at runtime — English is embedded).

## Fyne built-in widget strings (the chrome around our content)

Our `xx.json` localizes the app's own content. Fyne's built-in widgets ("Error" dialog title, OK/Cancel/Yes/No buttons, file picker chrome, calendar weekday names) come from Fyne's separate translation bundles. At startup we apply a [Fyne-localizer override](../../internal/i18n/fyne_override.go) that forces Fyne to render those chrome strings in the user's chosen app language — but only for languages Fyne itself ships a bundle for.

When adding a new app language:

1. Check whether [Fyne ships a translation bundle](https://github.com/fyne-io/fyne/tree/master/lang/translations) for your locale. The current list (Fyne v2.7.4): cs, de, el, en, es, fr, ja, pl, pt, pt_BR, ru, sv, ta, uk, zh_Hans.
2. **If yes:** copy the matching `base.xx.json` from Fyne's repo to [`internal/i18n/fyne_bundles/xx.json`](../../internal/i18n/fyne_bundles/) (no `base.` prefix in the destination filename). Re-run `go test ./internal/i18n/` to confirm. Users will see fully-localized Fyne chrome.
3. **If no** (e.g. our `af` and `hb` packs): nothing to do here. The override is a documented no-op for your locale; Fyne falls back to English for its chrome while our content renders in your language.

## Where to put files

The app resolves `assets/i18n` in this order:

1. Next to the executable
2. On macOS, under `Contents/Resources/assets/i18n` inside the `.app` bundle
3. Current working directory (`./assets/i18n`)

Ship or copy your files into that folder and restart the app. The language is chosen in **View → Preferences → Language** (or with `-lang xx` for one run) and applies after a restart.

## Display name in Preferences

The language picker shows a **native** label from **your** pack, not from English. In `xx.json`, include **either**:

- `locale_labels.xx` — e.g. for `el.json`: `"el": "Ελληνικά"`, or
- `locale_labels.self` — e.g. `"self": "Ελληνικά"` (same effect for that pack).

You do **not** need a full `locale_labels` matrix in every file; a single entry for your locale is enough. Optional `_comment` keys under `locale_labels` are ignored by the app.

## Verify / select a language

- **Preferences:** View → Preferences → **Language** lists every loaded pack (English first). Pick one and restart.
- **Command line (one run):** `KrankyBearMediaPlayer -lang xx` sets the UI language for that launch and saves it as the preference. Unknown codes fall back to English.

---

## Maintainer automation (`tools/` + the `i18n-translate` skill)

Two complementary paths keep this painless — the LLM handles **prose**, Python keeps the **JSON catalog** honest (chat models silently drop keys, so JSON parity is never trusted to the model).

### Long-form `.txt`/`.md` — the `i18n-translate` skill (in VS Code)
Ask Claude Code to translate a source file (e.g. *"translate help_en.txt"*). The [`i18n-translate` skill](../../.claude/skills/i18n-translate/SKILL.md) auto-detects target languages from the `_xx` suffixes already in this folder, translates the full document preserving structure/placeholders, and writes `<base>_<xx>.<ext>` back here. (The original external-AI prompts are still below for paste-into-a-browser use.)

### JSON catalog + embedding — `tools/*.py`
Run from the repo root:

| Command | Does |
|---------|------|
| `python3 tools/i18n_merge_from_en.py check` | Report each locale's missing/extra keys vs `en.json` (exit 1 if any missing). Compares **key sets**, not line counts. |
| `python3 tools/i18n_merge_from_en.py merge` | Backfill missing keys into every `xx.json` from `en.json` (English placeholder; never overwrites existing translations). |
| `python3 tools/i18n_audit.py [--locale xx] [--prefix p]` | List leaf strings still identical to English (likely untranslated). Expect some: `App`/`Bal`, percentages, EQ preset names, proper nouns. |
| `python3 tools/i18n_sync_embedded_defaults.py` | Copy `en.json` + `*_en.txt` (+ `LICENSE` → `license_en.txt`) into `internal/i18n/embedded_defaults/` for `go:embed`. |
| `python3 tools/apply_i18n_staged.py` | Deep-merge partial `tools/i18n_staged/<xx>.json` (e.g. an external AI's partial reply) into the full `xx.json`. |

`assets/i18n/` is the **authoring source of truth**; `internal/i18n/embedded_defaults/` is generated — edit the former, never the latter by hand.

### Build hook
[`tools/i18n_sync_for_build.sh`](../../tools/i18n_sync_for_build.sh) runs **merge → sync embedded → check parity** and is invoked automatically by `compile-mac.sh`. Linux/Windows compile scripts skip it (older build-VM Python); they pick up the refreshed `embedded_defaults/` via rsync from the Mac.

**Adding a language, end to end:** create `xx.json` (skill or AI prompt below) → `merge` to backfill stragglers → translate the long-form `.txt` via the skill → `audit` to spot gaps → `./tools/i18n_sync_for_build.sh` (or just `./compile-mac.sh`).

---

## AI prompt: translate `en.json` into `xx.json`

Attach **`en.json`** (the source catalog) and adapt the target language in the prompt.

You are a technical translator for a desktop music-library application. Target language: **(your language name)**; ISO 639-1 code: **(two letters, e.g. el)**.

**Task:** Produce a complete JSON message catalog for the app. Translate every string value; keep every key path identical to the source. Do not remove keys or sections.

**Hard rules**

- Output: **one** valid JSON object only (no preamble, no markdown fences unless asked for a paste-ready block).
- Preserve key order and structure exactly (nested objects, same dot-path semantics as the source).
- Preserve placeholders such as `{{app_name}}`, `{{n}}`, `{{title}}`, `{{artist}}`, `{{version}}` exactly — do not translate or reorder the text inside `{{ }}`.
- Preserve product tokens verbatim: the app name (`KrankyBear MediaPlayer`), filename-pattern tokens (`%track%`, `%title%`, `%artist%`, `%album%`, `%albumartist%`, `%year%`, `%genre%`), file names (`ReleaseNotes.txt`, `.m3u8`), CLI flags (`-lang`, `-db`, `KBMP_DB`), URLs, and audio terms used as proper nouns (ReplayGain, MP3, FLAC, OGG, WAV).
- Keep symbol/abbreviation values as-is where translation would not help (e.g. `cols.select` `"✓"`, `cols.track` `"#"`, `transport.rate_star` `"{{n}}★"`).
- Set `locale_labels.XX` (where `XX` is your file's code) or `locale_labels.self` to the **endonym** for your language (how native speakers name it).
- Use UTF-8. Escape JSON as required.

**Quality bar:** If the output has far fewer keys than the source, you have dropped content — fix before returning. Watch label width: very long translations of short labels (e.g. `transport.vol_sys` "Sys", column headers) may overflow narrow UI slots — prefer concise equivalents.

---

## AI prompt: translate the long-form `.txt` files

Attach the English source (e.g. **`help_en.txt`**) and translate into **`help_xx.txt`** (likewise `about_en.txt`, `updates_en.txt`, `license_en.txt`).

You are a technical translator for a desktop music-library application. Target language: **(your language name)**.

**Task:** Translate the attached document in full. Do not summarize, shorten, or merge sections.

**Hard rules**

- Preserve structure exactly: same section headings, the `━━━` separator lines, blank lines, and bullet markers (`•`, `✨`, `⚠️`).
- Preserve placeholders (`{{app_name}}`, `{{version}}`, `{{copyright}}`, `{{releases_url}}`) and product tokens (app name, `%track%`-style tokens, file names, URLs, ReplayGain/MP3/FLAC/OGG/WAV) unchanged.
- Output: a single plain-text document, no preamble like "Here is the translation…".

**Quality bar:** If the output is shorter than ~85% of the source line count, you have almost certainly omitted content — fix before returning.
