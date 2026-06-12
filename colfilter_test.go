package main

import (
	"path/filepath"
	"testing"
)

// TestColumnFilters checks the per-column filter clauses: substring match on text
// columns, GLOB pattern on Year, and that multiple filters AND together.
func TestColumnFilters(t *testing.T) {
	tmp := t.TempDir()
	db, err := openDB(filepath.Join(tmp, "lib.db"))
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	defer db.Close()

	fid, err := db.AddFolder(tmp, true)
	if err != nil {
		t.Fatalf("AddFolder: %v", err)
	}
	mk := func(rel, artist, album, genre string, year int) {
		tr := &Track{FolderID: fid, RelPath: rel, Title: rel, Artist: artist, Album: album, Genre: genre, Year: year}
		if err := db.UpsertTrack(tr); err != nil {
			t.Fatalf("upsert %s: %v", rel, err)
		}
	}
	mk("a.mp3", "Phil Wickham", "Hymn of Heaven", "Worship", 2021)
	mk("b.mp3", "Chris Tomlin", "Holy Roar", "Worship", 2018)
	mk("c.mp3", "Skillet", "Dominion", "Rock", 2024)
	mk("d.mp3", "Skillet", "Victorious", "Rock", 2019)

	count := func(q TrackQuery) int {
		q.Filter, q.SortCol = FilterAll, -1
		got, err := db.Tracks(q)
		if err != nil {
			t.Fatalf("Tracks: %v", err)
		}
		return len(got)
	}

	cases := []struct {
		name string
		q    TrackQuery
		want int
	}{
		{"artist substring, case-insensitive", TrackQuery{FArtist: "wickham"}, 1},
		{"genre substring", TrackQuery{FGenre: "rock"}, 2},
		{"album substring", TrackQuery{FAlbum: "victor"}, 1},
		{"artist AND genre", TrackQuery{FArtist: "Skillet", FGenre: "Rock"}, 2},
		{"year GLOB class 201[89]", TrackQuery{FYear: "201[89]"}, 2}, // 2018, 2019
		{"year GLOB wildcard 202*", TrackQuery{FYear: "202*"}, 2},    // 2021, 2024
		{"year exact 2024", TrackQuery{FYear: "2024"}, 1},
		{"no matches", TrackQuery{FArtist: "nobody"}, 0},
	}
	for _, c := range cases {
		if got := count(c.q); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}

	// Plays filter (GLOB on play count): bump one track to 5 plays.
	all, _ := db.Tracks(TrackQuery{Filter: FilterAll, SortCol: -1})
	for i := 0; i < 5; i++ {
		if err := db.IncrementPlayCount(all[0].ID); err != nil {
			t.Fatal(err)
		}
	}
	if got := count(TrackQuery{FPlays: "5"}); got != 1 {
		t.Errorf("FPlays=5: got %d, want 1", got)
	}
	if got := count(TrackQuery{FPlays: "0"}); got != 3 {
		t.Errorf("FPlays=0 (unplayed): got %d, want 3", got)
	}
}
