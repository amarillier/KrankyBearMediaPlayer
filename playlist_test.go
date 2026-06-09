package main

import (
	"bytes"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestPlaylistM3URoundTrip writes tracks to an extended M3U with paths relative
// to the playlist's directory, then parses them back and confirms they resolve
// to the original absolute paths.
func TestPlaylistM3URoundTrip(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "Music")
	tracks := []Track{
		{FolderRoot: root, RelPath: "a/song1.mp3", Title: "Song One", Artist: "Artist A", Duration: 100},
		{FolderRoot: root, RelPath: "b/song2.flac", Title: "Song Two", Duration: 0}, // no artist
	}

	var buf bytes.Buffer
	if err := writePlaylistM3U(&buf, base, tracks); err != nil {
		t.Fatalf("write: %v", err)
	}
	out := buf.String()

	if !strings.HasPrefix(out, "#EXTM3U\n") {
		t.Fatalf("missing #EXTM3U header:\n%s", out)
	}
	if !strings.Contains(out, "#EXTINF:100,Artist A - Song One\n") {
		t.Errorf("missing/incorrect EXTINF with artist:\n%s", out)
	}
	if !strings.Contains(out, "#EXTINF:0,Song Two\n") {
		t.Errorf("missing/incorrect EXTINF without artist:\n%s", out)
	}
	// Paths are written relative to base, forward-slashed.
	if !strings.Contains(out, "\nMusic/a/song1.mp3\n") {
		t.Errorf("expected relative forward-slash path, got:\n%s", out)
	}

	paths := parsePlaylistM3U(strings.NewReader(out), base)
	want := []string{
		filepath.Clean(tracks[0].AbsPath()),
		filepath.Clean(tracks[1].AbsPath()),
	}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("parsed paths = %v, want %v", paths, want)
	}
}

// TestParsePlaylistSkipsCommentsAndBlanks checks that #-comments and blank lines
// are ignored and that already-absolute entries are kept as-is.
func TestParsePlaylistSkipsCommentsAndBlanks(t *testing.T) {
	base := t.TempDir()
	abs := filepath.Join(base, "elsewhere", "track.ogg")
	content := "#EXTM3U\n\n#EXTINF:42,Some Song\nrel/track.mp3\n\n" + filepath.ToSlash(abs) + "\n"

	paths := parsePlaylistM3U(strings.NewReader(content), base)
	want := []string{
		filepath.Join(base, "rel", "track.mp3"),
		filepath.Clean(abs),
	}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("parsed = %v, want %v", paths, want)
	}
}
