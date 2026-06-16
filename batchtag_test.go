package main

import "testing"

func TestTagPatchAny(t *testing.T) {
	if (tagPatch{}).any() {
		t.Error("empty patch should report no fields")
	}
	if !(tagPatch{setGenre: true}).any() {
		t.Error("a flagged field should report any()==true")
	}
}

// TestTagPatchApplyToOnlyTouchesFlagged verifies the merge only overwrites the
// ticked fields and leaves everything else (title, track, art, unticked fields)
// exactly as read from the file.
func TestTagPatchApplyToOnlyTouchesFlagged(t *testing.T) {
	orig := trackTags{
		Title:       "Original Title",
		Artist:      "Original Artist",
		Album:       "Original Album",
		AlbumArtist: "Original AA",
		Genre:       "Rock",
		Comment:     "orig comment",
		Year:        1999,
		Track:       3,
		Art:         []byte{1, 2, 3},
		ArtMIME:     "image/png",
	}
	patch := tagPatch{
		setAlbum:       true,
		album:          "New Album",
		setAlbumArtist: true,
		albumArtist:    "Various",
		setYear:        true,
		year:           2024,
	}
	got := orig
	patch.applyTo(&got, "/music/song.mp3")

	// Changed:
	if got.Album != "New Album" || got.AlbumArtist != "Various" || got.Year != 2024 {
		t.Errorf("flagged fields not applied: %+v", got)
	}
	// Untouched:
	if got.Title != orig.Title || got.Artist != orig.Artist || got.Genre != orig.Genre ||
		got.Comment != orig.Comment || got.Track != orig.Track || string(got.Art) != string(orig.Art) {
		t.Errorf("unflagged fields were modified: %+v", got)
	}
}

// TestTagPatchApplyToFromPattern verifies per-file pattern parsing fills the matched
// fields, that fixed fields override the pattern, and that a non-matching filename
// leaves fields untouched.
func TestTagPatchApplyToFromPattern(t *testing.T) {
	base := trackTags{Title: "old", Artist: "old", Track: 1}

	// Pattern fills title/artist/track from the name.
	got := base
	p := tagPatch{usePattern: true, pattern: "%track% %title% - %artist%"}
	p.applyTo(&got, "/music/07 My Song - The Band.mp3")
	if got.Title != "My Song" || got.Artist != "The Band" || got.Track != 7 {
		t.Errorf("pattern not applied: %+v", got)
	}

	// Fixed field overrides the pattern's artist.
	got = base
	p = tagPatch{usePattern: true, pattern: "%track% %title% - %artist%",
		setArtist: true, artist: "Override"}
	p.applyTo(&got, "/music/07 My Song - The Band.mp3")
	if got.Artist != "Override" {
		t.Errorf("fixed field should override pattern, got artist=%q", got.Artist)
	}

	// Non-matching filename leaves everything as-is.
	got = base
	p = tagPatch{usePattern: true, pattern: "%track% - %title%"}
	p.applyTo(&got, "/music/no separators here.mp3")
	if got.Title != base.Title || got.Artist != base.Artist || got.Track != base.Track {
		t.Errorf("non-matching pattern should change nothing, got %+v", got)
	}
}

func TestTagPatchAnyPatternAndArt(t *testing.T) {
	if !(tagPatch{usePattern: true}).any() {
		t.Error("usePattern should make any()==true")
	}
	if !(tagPatch{artMode: artSet}).any() {
		t.Error("artMode set should make any()==true")
	}
	if (tagPatch{artMode: artLeave}).any() {
		t.Error("artLeave with nothing else should be any()==false")
	}
}

// TestTagPatchFindReplace covers find-and-replace within one field, case-sensitive
// and insensitive, that other fields are untouched, and the empty-find no-op.
func TestTagPatchFindReplace(t *testing.T) {
	base := trackTags{Title: "Song feat. Bob", Artist: "A FEAT. B"}

	// Case-sensitive on Title; Artist untouched.
	got := base
	tagPatch{frUse: true, frField: "Title", frFind: "feat.", frReplace: "ft."}.applyTo(&got, "/x.mp3")
	if got.Title != "Song ft. Bob" {
		t.Errorf("case-sensitive replace: got %q", got.Title)
	}
	if got.Artist != base.Artist {
		t.Errorf("other field changed: %q", got.Artist)
	}

	// Case-insensitive on Artist matches the uppercase "FEAT.".
	got = base
	tagPatch{frUse: true, frField: "Artist", frFind: "feat.", frReplace: "ft.", frCI: true}.applyTo(&got, "/x.mp3")
	if got.Artist != "A ft. B" {
		t.Errorf("case-insensitive replace: got %q", got.Artist)
	}

	// Empty find is a no-op and doesn't count as a change.
	if (tagPatch{frUse: true, frFind: ""}).any() {
		t.Error("empty find should not make any()==true")
	}
	if !(tagPatch{frUse: true, frFind: "x"}).any() {
		t.Error("frUse with a find term should make any()==true")
	}
}
