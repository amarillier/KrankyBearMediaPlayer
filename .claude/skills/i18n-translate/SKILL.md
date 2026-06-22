---
name: i18n-translate
description: >
  Translates an English source file into all supported languages for a project, by
  auto-detecting target languages from existing files in the assets/i18n/ directory.
  Use this skill whenever the user asks to translate a file, localize content, generate
  i18n translations, or says something like "translate ReleaseNotes.txt" or "add this
  file to i18n". Also trigger when the user provides a file path and asks for it to be
  translated or localized into other languages — even if they don't explicitly mention
  "i18n" or "assets/i18n".
---

# i18n Translate

Translate an English source file into all languages currently supported by the project,
detecting those languages automatically from the existing `assets/i18n/` directory.

## Workflow

### Step 1 — Locate the source file

Read the file path the user provided. Resolve it relative to the user's working directory
if it's a relative path. Read the file contents in full.

### Step 2 — Find the i18n directory

Look for `assets/i18n/` relative to the source file's parent directory. For example:
- Source: `./ReleaseNotes.txt` → i18n dir: `./assets/i18n/`
- Source: `src/docs/Changelog.txt` → i18n dir: `src/docs/assets/i18n/` *(try this first)*,
  falling back to `./assets/i18n/` if not found

If `assets/i18n/` doesn't exist at all, tell the user and stop — ask them to confirm the
path before proceeding.

### Step 3 — Detect supported languages

List all files in `assets/i18n/` and extract language codes from filenames. The pattern is:
`<basename>_<langcode>.<ext>` — e.g. `about_de.txt` → `de`, `home_zh_CN.txt` → `zh_CN`.

Use a regex like `_([a-z]{2}(?:_[A-Z]{2})?)\.` to extract codes. Deduplicate the results.
These are the target languages for this translation run.

If no language codes are found, tell the user: "No existing translations found in
assets/i18n/ — I can't auto-detect which languages to use. Please let me know which
language codes to target (e.g. `ja`, `de`, `es`)."

### Step 4 — Translate

For each detected language code, translate the full source content into that language.

Translation guidelines:
- Preserve the structure and formatting of the original (line breaks, bullet points,
  headers, markdown, etc.)
- Keep any technical terms, product names, version numbers, and code snippets untranslated
- Adapt idiomatic expressions naturally — don't translate them word-for-word
- If the source contains placeholders like `{variable}` or `%s`, leave them as-is
- Aim for a natural, professional tone appropriate for software documentation or release notes

### Step 5 — Write output files

Derive the output filename by inserting the language code before the extension:
`<basename>_<langcode>.<ext>`

Write each file to `assets/i18n/` in the same directory where you found it.

Example:
- Source: `./ReleaseNotes.txt`, language `ja` → `./assets/i18n/ReleaseNotes_ja.txt`
- Source: `./ReleaseNotes.txt`, language `de` → `./assets/i18n/ReleaseNotes_de.txt`

### Step 6 — Report

After writing all files, give the user a concise summary:

```
Translated ReleaseNotes.txt into 4 languages:
  ✓ assets/i18n/ReleaseNotes_de.txt  (German)
  ✓ assets/i18n/ReleaseNotes_es.txt  (Spanish)
  ✓ assets/i18n/ReleaseNotes_ja.txt  (Japanese)
  ✓ assets/i18n/ReleaseNotes_fr.txt  (French)
```

If any translation failed or a file couldn't be written, note it clearly.

---

## Edge cases

- **File already exists**: Overwrite it — the user asked for a fresh translation.
- **Large files**: Translate in full regardless of length. Don't truncate.
- **Non-.txt files**: The same workflow applies to `.md`, `.html`, `.json` string files,
  etc. Preserve the format precisely (e.g. keep JSON keys untranslated, only translate values).
- **Unknown language code**: Use your best knowledge of the ISO 639 standard. If genuinely
  ambiguous, translate anyway and note the assumed language in the summary.
