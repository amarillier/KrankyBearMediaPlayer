package main

import (
	"strings"
	"testing"
)

// dupFixture: "Hey Jude" appears twice (different albums), once via a differently-cased
// artist/title (should still match Artist+Title); "Yesterday" is unique; two distinct
// songs share the filename "01.mp3" in different folders.
func dupFixture() []Track {
	return []Track{
		{Artist: "The Beatles", Title: "Hey Jude", Album: "Single", Year: 1968, RelPath: "a/01.mp3", PlayCount: 3},
		{Artist: "the beatles", Title: "hey jude", Album: "1967-1970", Year: 1973, RelPath: "b/05.mp3", PlayCount: 1},
		{Artist: "The Beatles", Title: "Yesterday", Album: "Help!", Year: 1965, RelPath: "c/13.mp3"},
		{Artist: "Queen", Title: "Bohemian Rhapsody", Album: "Opera", Year: 1975, RelPath: "d/01.mp3"},
		{Artist: "", Title: "", Album: "", RelPath: "e/untagged.flac"}, // no title → skipped by tag modes
	}
}

func TestDuplicatesArtistTitle(t *testing.T) {
	_, rows, _, summary := buildDuplicatesReport(dupFixture(), dupArtistTitle, "Name", false)
	// Only the two "Hey Jude" rows should appear (case-insensitive match across albums).
	if len(rows) != 2 {
		t.Fatalf("Artist+Title: got %d rows, want 2: %v", len(rows), rows)
	}
	// Matching is case-insensitive, but each row shows its file's own raw title, so the
	// second copy legitimately reads "hey jude".
	for _, r := range rows {
		if !strings.EqualFold(r[1], "Hey Jude") {
			t.Errorf("unexpected duplicate row title %q", r[1])
		}
	}
	if summary != "1 duplicate sets · 2 tracks" {
		t.Errorf("summary = %q", summary)
	}
}

func TestDuplicatesArtistTitleAlbum(t *testing.T) {
	// The two "Hey Jude" tracks are on different albums, so strict mode finds NO duplicates.
	_, rows, _, summary := buildDuplicatesReport(dupFixture(), dupArtistTitleAlbum, "Name", false)
	if len(rows) != 0 {
		t.Fatalf("Artist+Title+Album: got %d rows, want 0: %v", len(rows), rows)
	}
	if summary != "0 duplicate sets · 0 tracks" {
		t.Errorf("summary = %q", summary)
	}
}

func TestDuplicatesFilename(t *testing.T) {
	// "01.mp3" appears in two folders (Beatles a/01.mp3 + Queen d/01.mp3) → one dup set.
	_, rows, _, summary := buildDuplicatesReport(dupFixture(), dupFilename, "Name", false)
	if len(rows) != 2 {
		t.Fatalf("Filename: got %d rows, want 2: %v", len(rows), rows)
	}
	if summary != "1 duplicate sets · 2 tracks" {
		t.Errorf("summary = %q", summary)
	}
}
