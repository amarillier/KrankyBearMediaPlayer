package main

import (
	"path/filepath"
	"testing"
)

// newTestDBWithTracks creates a DB with one folder and the given (genre, artist,
// album) tracks, returning the DB. Paths/titles are synthesised per index.
func newTestDBWithTracks(t *testing.T, specs []Track) *DB {
	t.Helper()
	db, err := openDB(filepath.Join(t.TempDir(), "lib.db"))
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	fid, err := db.AddFolder(t.TempDir(), true)
	if err != nil {
		t.Fatalf("AddFolder: %v", err)
	}
	for i := range specs {
		tr := specs[i]
		tr.FolderID = fid
		if tr.RelPath == "" {
			tr.RelPath = filepath.ToSlash(filepath.Join("d", tr.Title+".mp3"))
		}
		if err := db.UpsertTrack(&tr); err != nil {
			t.Fatalf("UpsertTrack: %v", err)
		}
	}
	return db
}

// TestTrackQueryCriteria checks the exact-match genre/artist/album constraints
// (case-insensitive) added for smart playlists, including combining them.
func TestTrackQueryCriteria(t *testing.T) {
	db := newTestDBWithTracks(t, []Track{
		{Title: "a", Genre: "Rock", Artist: "Queen", Album: "News"},
		{Title: "b", Genre: "rock", Artist: "Queen", Album: "Opera"},
		{Title: "c", Genre: "Jazz", Artist: "Davis", Album: "Blue"},
	})
	defer db.Close()

	// Genre is case-insensitive: "rock" matches both Rock and rock.
	if got, _ := db.Tracks(TrackQuery{Filter: FilterAll, Genre: "rock", SortCol: -1}); len(got) != 2 {
		t.Fatalf("Genre=rock expected 2, got %d", len(got))
	}
	// Partial match: a substring of the artist matches (e.g. "ueen" -> "Queen").
	if got, _ := db.Tracks(TrackQuery{Filter: FilterAll, Artist: "ueen", SortCol: -1}); len(got) != 2 {
		t.Fatalf("Artist~ueen expected 2 (partial), got %d", len(got))
	}
	// Genre AND artist AND album narrows to one (partial album "per" -> "Opera").
	got, _ := db.Tracks(TrackQuery{Filter: FilterAll, Genre: "Rock", Artist: "Queen", Album: "per", SortCol: -1})
	if len(got) != 1 || got[0].Title != "b" {
		t.Fatalf("combined criteria expected track b, got %v", got)
	}
	// A constraint with no matches yields nothing.
	if got, _ := db.Tracks(TrackQuery{Filter: FilterAll, Artist: "Nobody", SortCol: -1}); len(got) != 0 {
		t.Fatalf("Artist=Nobody expected 0, got %d", len(got))
	}
	// Wildcards in the text are matched literally (escaped), not as patterns.
	if got, _ := db.Tracks(TrackQuery{Filter: FilterAll, Artist: "%", SortCol: -1}); len(got) != 0 {
		t.Fatalf("Artist=%% should match nothing literally, got %d", len(got))
	}
}

// TestSmartPlaylistCRUD covers save (insert + upsert by name), list, and delete.
func TestSmartPlaylistCRUD(t *testing.T) {
	db := newTestDBWithTracks(t, nil)
	defer db.Close()

	if err := db.SaveSmartPlaylist(SmartPlaylist{Name: "Faves", Filter: FilterAtLeast4, Genre: "Rock"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := db.SaveSmartPlaylist(SmartPlaylist{Name: "Jazz", Genre: "Jazz"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	// Re-saving the same name updates rather than duplicating.
	if err := db.SaveSmartPlaylist(SmartPlaylist{Name: "Faves", Filter: FilterExactly5, Artist: "Queen"}); err != nil {
		t.Fatalf("re-save: %v", err)
	}

	lists, err := db.SmartPlaylists()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(lists) != 2 {
		t.Fatalf("expected 2 playlists after upsert, got %d", len(lists))
	}
	// Ordered by name: Faves then Jazz. Faves should reflect the updated criteria.
	if lists[0].Name != "Faves" || lists[0].Filter != FilterExactly5 || lists[0].Artist != "Queen" || lists[0].Genre != "" {
		t.Fatalf("Faves not updated correctly: %+v", lists[0])
	}

	if err := db.DeleteSmartPlaylist(lists[0].ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if remaining, _ := db.SmartPlaylists(); len(remaining) != 1 || remaining[0].Name != "Jazz" {
		t.Fatalf("after delete expected only Jazz, got %v", remaining)
	}
}

// TestUpdateSmartPlaylist covers editing by id, including a rename (which the
// name-keyed SaveSmartPlaylist upsert can't do without orphaning the old row).
func TestUpdateSmartPlaylist(t *testing.T) {
	db := newTestDBWithTracks(t, nil)
	defer db.Close()

	if err := db.SaveSmartPlaylist(SmartPlaylist{Name: "Old Name", Filter: FilterAtLeast4, Genre: "Rock"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	lists, _ := db.SmartPlaylists()
	id := lists[0].ID

	// Rename + change criteria by id.
	if err := db.UpdateSmartPlaylist(SmartPlaylist{ID: id, Name: "New Name", Filter: FilterExactly5, Artist: "Queen"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	lists, _ = db.SmartPlaylists()
	if len(lists) != 1 {
		t.Fatalf("expected 1 playlist (renamed, not duplicated), got %d", len(lists))
	}
	got := lists[0]
	if got.ID != id || got.Name != "New Name" || got.Filter != FilterExactly5 || got.Artist != "Queen" || got.Genre != "" {
		t.Fatalf("update not applied correctly: %+v", got)
	}
}
