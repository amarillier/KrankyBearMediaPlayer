package main

import (
	"testing"

	"mediaplayer/internal/i18n"
)

// TestLocalePacksLoad verifies the on-disk locale packs in assets/i18n (hb, af) are
// discovered by mergeI18nFromAssets, appear in the language picker, and actually
// translate a representative menu key. Run from the package dir so the cwd-relative
// "assets/i18n" search path resolves.
func TestLocalePacksLoad(t *testing.T) {
	if err := i18n.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	mergeI18nFromAssets()

	for _, code := range []string{"hb", "af"} {
		if !i18n.HasCatalog(code) {
			t.Errorf("locale pack %q not loaded from assets/i18n", code)
		}
	}

	// Display names come from each pack's own locale_labels.
	if got := i18n.LocaleDisplayName("hb"); got != "Hillbilly" {
		t.Errorf("LocaleDisplayName(hb) = %q, want Hillbilly", got)
	}
	if got := i18n.LocaleDisplayName("af"); got != "Afrikaans" {
		t.Errorf("LocaleDisplayName(af) = %q, want Afrikaans", got)
	}

	// Representative migrated keys (Slice 1 menu, Slice 2a chrome) must differ from
	// English in each pack — guards against a pack missing a whole section. Keys are
	// chosen to differ from English in BOTH packs (some words, e.g. "Play"/"Album",
	// legitimately translate to themselves, so they can't prove presence this way).
	keys := []string{"menu.library.label", "cols.artist", "toolbar.rescan", "ctx.add_to_queue", "transport.nothing_playing", "filter.rating.all", "preferences.count_end_of_track", "smart.summary_all"}
	for _, key := range keys {
		en := i18n.TCInLocale("en", key, nil)
		for _, code := range []string{"hb", "af"} {
			if got := i18n.TCInLocale(code, key, nil); got == en {
				t.Errorf("%s not translated in %q (still %q)", key, code, got)
			}
		}
	}

	// The picker lists English first, then the packs.
	labels, codeByLabel := uiLanguageOptions()
	if len(labels) < 3 {
		t.Errorf("uiLanguageOptions returned %d labels, want >=3 (en, af, hb)", len(labels))
	}
	if codeByLabel["English"] != "en" {
		t.Errorf("picker missing English → en mapping: %v", codeByLabel)
	}
}
