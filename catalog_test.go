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

	// Unplayed filter: both tracks, no plays, no manual rating.
	if got, _ := db.Tracks(TrackQuery{Filter: FilterUnplayed, SortCol: -1}); len(got) != 2 {
		t.Fatalf("FilterUnplayed expected 2, got %d", len(got))
	}

	// Play song1 three times -> auto rating 3.
	for i := 0; i < 3; i++ {
		if err := db.IncrementPlayCount(song1.ID); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := db.Tracks(TrackQuery{Filter: FilterExactly3, SortCol: -1})
	if len(got) != 1 || got[0].ID != song1.ID {
		t.Fatalf("FilterExactly3 expected song1, got %v", got)
	}
	if got[0].EffectiveRating() != 3 {
		t.Fatalf("auto rating expected 3, got %d", got[0].EffectiveRating())
	}

	// Manual rating overrides auto: set song1 to 5 stars.
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
