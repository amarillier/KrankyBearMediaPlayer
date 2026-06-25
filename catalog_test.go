package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// writeMinimalWAV writes a tiny valid 8-bit mono PCM WAV so the scanner has a
// real supported-extension file to catalog. (We only test cataloging here, not
// decoding.)
func writeMinimalWAV(t *testing.T, path string) {
	t.Helper()
	const sampleRate = 8000
	data := make([]byte, 16) // 16 samples of silence
	for i := range data {
		data[i] = 128
	}
	var buf []byte
	put := func(b ...byte) { buf = append(buf, b...) }
	putU32 := func(v uint32) {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], v)
		buf = append(buf, b[:]...)
	}
	putU16 := func(v uint16) {
		var b [2]byte
		binary.LittleEndian.PutUint16(b[:], v)
		buf = append(buf, b[:]...)
	}
	put('R', 'I', 'F', 'F')
	putU32(uint32(36 + len(data)))
	put('W', 'A', 'V', 'E')
	put('f', 'm', 't', ' ')
	putU32(16)
	putU16(1) // PCM
	putU16(1) // mono
	putU32(sampleRate)
	putU32(sampleRate) // byte rate
	putU16(1)          // block align
	putU16(8)          // bits per sample
	put('d', 'a', 't', 'a')
	putU32(uint32(len(data)))
	buf = append(buf, data...)
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatalf("write wav: %v", err)
	}
}

func TestCatalogScanRatingAndRelocate(t *testing.T) {
	tmp := t.TempDir()
	musicA := filepath.Join(tmp, "driveD", "Music")
	if err := os.MkdirAll(filepath.Join(musicA, "Rock"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeMinimalWAV(t, filepath.Join(musicA, "song1.wav"))
	writeMinimalWAV(t, filepath.Join(musicA, "Rock", "song2.wav"))

	db, err := openDB(filepath.Join(tmp, "lib.db"))
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	defer db.Close()

	// Add + scan the folder.
	fid, err := db.AddFolder(musicA, true)
	if err != nil {
		t.Fatalf("AddFolder: %v", err)
	}
	res, err := db.ScanFolder(Folder{ID: fid, Path: musicA, Recursive: true}, nil)
	if err != nil {
		t.Fatalf("ScanFolder: %v", err)
	}
	if res.Updated != 2 {
		t.Fatalf("expected 2 catalogued tracks, got %d (errors=%d)", res.Updated, res.Errors)
	}

	tracks, err := db.Tracks(TrackQuery{Filter: FilterAll, SortCol: -1})
	if err != nil {
		t.Fatalf("Tracks: %v", err)
	}
	if len(tracks) != 2 {
		t.Fatalf("expected 2 tracks, got %d", len(tracks))
	}

	// Relative paths must be stored, and AbsPath must reconstruct correctly.
	var song1 Track
	for _, tr := range tracks {
		if tr.RelPath == "song1.wav" {
			song1 = tr
		}
		if filepath.IsAbs(tr.RelPath) {
			t.Errorf("rel_path should be relative, got %q", tr.RelPath)
		}
		if _, err := os.Stat(tr.AbsPath()); err != nil {
			t.Errorf("AbsPath %q not found: %v", tr.AbsPath(), err)
		}
	}
	if song1.ID == 0 {
		t.Fatal("song1.wav not catalogued")
	}

	// Unrated filter: both tracks, no manual rating yet.
	if got, _ := db.Tracks(TrackQuery{Filter: FilterUnrated, SortCol: -1}); len(got) != 2 {
		t.Fatalf("FilterUnrated expected 2, got %d", len(got))
	}

	// Ratings are manual-only: playing a track does NOT auto-star it.
	for i := 0; i < 3; i++ {
		if err := db.IncrementPlayCount(song1.ID); err != nil {
			t.Fatal(err)
		}
	}
	played, _ := db.Tracks(TrackQuery{Filter: FilterAll, Search: "song1", SortCol: -1})
	if len(played) != 1 || played[0].PlayCount != 3 {
		t.Fatalf("expected song1 with 3 plays, got %v", played)
	}
	if played[0].EffectiveRating() != 0 {
		t.Fatalf("manual-only: 3 plays must not auto-rate, got %d", played[0].EffectiveRating())
	}
	// It must still be Unrated (plays don't create stars).
	if got, _ := db.Tracks(TrackQuery{Filter: FilterUnrated, SortCol: -1}); len(got) != 2 {
		t.Fatalf("after plays, FilterUnrated expected 2, got %d", len(got))
	}

	// A manual rating is what counts: set song1 to 5 stars.
	if err := db.SetRating(song1.ID, 5); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.Tracks(TrackQuery{Filter: FilterExactly5, SortCol: -1}); len(got) != 1 || got[0].ID != song1.ID {
		t.Fatalf("FilterExactly5 expected song1 after manual rating, got %v", got)
	}
	if got, _ := db.Tracks(TrackQuery{Filter: FilterAtLeast4, SortCol: -1}); len(got) != 1 {
		t.Fatalf("FilterAtLeast4 expected 1, got %d", len(got))
	}

	// Relocate: simulate the USB drive remounting at a different path.
	musicB := filepath.Join(tmp, "driveE", "Music")
	if err := os.MkdirAll(filepath.Join(musicB, "Rock"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeMinimalWAV(t, filepath.Join(musicB, "song1.wav"))
	writeMinimalWAV(t, filepath.Join(musicB, "Rock", "song2.wav"))

	if err := db.RelocateFolder(fid, musicB); err != nil {
		t.Fatalf("RelocateFolder: %v", err)
	}
	relocated, _ := db.Tracks(TrackQuery{Filter: FilterAll, SortCol: -1})
	for _, tr := range relocated {
		if _, err := os.Stat(tr.AbsPath()); err != nil {
			t.Errorf("after relocate, AbsPath %q not found: %v", tr.AbsPath(), err)
		}
	}
	// Play count + manual rating must survive the relocation.
	for _, tr := range relocated {
		if tr.ID == song1.ID {
			if tr.PlayCount != 3 {
				t.Errorf("play count lost on relocate: got %d", tr.PlayCount)
			}
			if tr.EffectiveRating() != 5 {
				t.Errorf("manual rating lost on relocate: got %d", tr.EffectiveRating())
			}
		}
	}
}

func TestVersionIsNewer(t *testing.T) {
	cases := []struct {
		local, remote string
		want          bool
	}{
		{"0.2.0", "0.1.0", true},   // local ahead -> HardHat
		{"0.1.0", "0.1.0", false},  // current
		{"0.1.0", "0.2.0", false},  // update available
		{"v0.2.0", "v0.1.0", true}, // tolerates 'v' prefix
		{"0.1.0", "", false},       // offline / no remote tag
		{"bad", "0.1.0", false},    // unparseable
	}
	for _, c := range cases {
		if got := versionIsNewer(c.local, c.remote); got != c.want {
			t.Errorf("versionIsNewer(%q,%q)=%v want %v", c.local, c.remote, got, c.want)
		}
	}
}

func TestOrderByMultiColumn(t *testing.T) {
	const tail = ", t.artist, t.album, t.track_no, t.title"
	cases := []struct {
		name string
		q    TrackQuery
		want string
	}{
		{"default", TrackQuery{SortCol: -1, Sort2Col: -1}, "t.artist, t.album, t.track_no, t.title"},
		{"primary asc", TrackQuery{SortCol: colTitle, Sort2Col: -1}, "t.title ASC" + tail},
		{"primary desc", TrackQuery{SortCol: colPlays, Desc: true, Sort2Col: -1}, "t.play_count DESC" + tail},
		{"album then track", TrackQuery{SortCol: colAlbum, Sort2Col: colTrack}, "t.album ASC, t.track_no ASC" + tail},
		{"rating desc then title", TrackQuery{SortCol: colRating, Desc: true, Sort2Col: colTitle}, effRatingExpr + " DESC, t.title ASC" + tail},
		{"secondary same as primary omitted", TrackQuery{SortCol: colTitle, Sort2Col: colTitle}, "t.title ASC" + tail},
	}
	for _, c := range cases {
		if got := orderBy(c.q); got != c.want {
			t.Errorf("%s: orderBy = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestCopyFileIntoCollision(t *testing.T) {
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "dst")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	// Two different source files sharing a base name (from different folders).
	srcA := filepath.Join(tmp, "a", "song.mp3")
	srcB := filepath.Join(tmp, "b", "song.mp3")
	for p, content := range map[string]string{srcA: "AAA", srcB: "BBB"} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := copyFileInto(srcA, dst); err != nil {
		t.Fatal(err)
	}
	if err := copyFileInto(srcB, dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "song.mp3")); string(b) != "AAA" {
		t.Errorf("song.mp3 = %q, want AAA", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "song (2).mp3")); string(b) != "BBB" {
		t.Errorf("song (2).mp3 = %q, want BBB (collision suffix)", b)
	}
}

// TestDeleteFolderCascade verifies removing a folder drops its tracks (FK cascade)
// and that CountTracksInFolder reports the right number first.
func TestDeleteFolderCascade(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "lib.db"))
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	defer db.Close()
	fid, err := db.AddFolder(t.TempDir(), true)
	if err != nil {
		t.Fatalf("AddFolder: %v", err)
	}
	for _, name := range []string{"a", "b", "c"} {
		if err := db.UpsertTrack(&Track{FolderID: fid, RelPath: name + ".mp3", Title: name}); err != nil {
			t.Fatalf("UpsertTrack: %v", err)
		}
	}
	if n, _ := db.CountTracksInFolder(fid); n != 3 {
		t.Fatalf("CountTracksInFolder = %d, want 3", n)
	}
	if err := db.DeleteFolder(fid); err != nil {
		t.Fatalf("DeleteFolder: %v", err)
	}
	if got, _ := db.Tracks(TrackQuery{Filter: FilterAll, SortCol: -1}); len(got) != 0 {
		t.Fatalf("after DeleteFolder expected 0 tracks (cascade), got %d", len(got))
	}
	if folders, _ := db.Folders(); len(folders) != 0 {
		t.Fatalf("expected 0 folders after delete, got %d", len(folders))
	}
}

func TestDeleteTrackCascade(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "lib.db"))
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	defer db.Close()
	fid, err := db.AddFolder(t.TempDir(), true)
	if err != nil {
		t.Fatalf("AddFolder: %v", err)
	}
	keep := &Track{FolderID: fid, RelPath: "keep.mp3", Title: "keep"}
	drop := &Track{FolderID: fid, RelPath: "drop.mp3", Title: "drop"}
	for _, tr := range []*Track{keep, drop} {
		if err := db.UpsertTrack(tr); err != nil {
			t.Fatalf("UpsertTrack: %v", err)
		}
	}
	// Give the doomed track a thumbnail so we can confirm the side table cascades.
	if err := db.SetThumb(drop.ID, []byte("png")); err != nil {
		t.Fatalf("SetThumb: %v", err)
	}

	if err := db.DeleteTrack(drop.ID); err != nil {
		t.Fatalf("DeleteTrack: %v", err)
	}

	got, _ := db.Tracks(TrackQuery{Filter: FilterAll, SortCol: -1})
	if len(got) != 1 || got[0].ID != keep.ID {
		t.Fatalf("after DeleteTrack expected only the kept track, got %d rows", len(got))
	}
	if db.Thumb(drop.ID) != nil {
		t.Fatalf("expected thumbnail to cascade-delete with the track")
	}
}
