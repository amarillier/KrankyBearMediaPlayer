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
	patch.applyTo(&got)

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
