package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/bogem/id3v2/v2"
)

// tinyPNG returns the bytes of a 1x1 PNG, a valid image the FLAC picture block
// can measure and the readers can round-trip.
func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{10, 20, 30, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// writeMinimalFLAC writes "fLaC" + a single last STREAMINFO block (34 zero
// bytes) + a stub frame whose sync code (0xFF 0xF8) satisfies go-flac's parser.
// That's enough for go-flac to parse and rewrite and for the readers to find
// the comment/picture blocks we add.
func writeMinimalFLAC(t *testing.T, path string) {
	t.Helper()
	buf := []byte{'f', 'L', 'a', 'C', 0x80, 0x00, 0x00, 0x22}
	buf = append(buf, make([]byte, 34)...)
	buf = append(buf, 0xFF, 0xF8) // frame sync code (no real audio)
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatalf("write flac: %v", err)
	}
}

func TestTagsWritable(t *testing.T) {
	cases := map[string]bool{"a.mp3": true, "b.flac": true, "c.MP3": true, "d.ogg": false, "e.wav": false}
	for name, want := range cases {
		if got := tagsWritable(name); got != want {
			t.Errorf("tagsWritable(%q) = %v, want %v", name, got, want)
		}
	}
}

// roundTrip writes tags to path then reads them back, asserting the text fields
// and embedded art survive. Used for both MP3 and FLAC.
func roundTrip(t *testing.T, path string, art []byte) {
	t.Helper()
	want := trackTags{
		Title:       "Some Title",
		Artist:      "Some Artist",
		Album:       "Some Album",
		AlbumArtist: "Various",
		Genre:       "Electronic",
		Comment:     "a note",
		Year:        2021,
		Track:       7,
		Art:         art,
		ArtMIME:     "image/png",
	}
	if err := writeTrackTags(path, want); err != nil {
		t.Fatalf("writeTrackTags: %v", err)
	}
	got, err := readTrackTags(path)
	if err != nil {
		t.Fatalf("readTrackTags: %v", err)
	}
	if got.Title != want.Title || got.Artist != want.Artist || got.Album != want.Album {
		t.Errorf("core fields: got %+v", got)
	}
	if got.AlbumArtist != want.AlbumArtist {
		t.Errorf("album artist: got %q want %q", got.AlbumArtist, want.AlbumArtist)
	}
	if got.Genre != want.Genre {
		t.Errorf("genre: got %q want %q", got.Genre, want.Genre)
	}
	if got.Comment != want.Comment {
		t.Errorf("comment: got %q want %q", got.Comment, want.Comment)
	}
	if got.Year != want.Year {
		t.Errorf("year: got %d want %d", got.Year, want.Year)
	}
	if got.Track != want.Track {
		t.Errorf("track: got %d want %d", got.Track, want.Track)
	}
	if !bytes.Equal(got.Art, want.Art) {
		t.Errorf("art bytes differ: got %d bytes, want %d", len(got.Art), len(want.Art))
	}
}

// TestUTF16MP3SurvivesWrite guards against the bogem/id3v2 UTF-16 corruption bug:
// a tag with a UTF-16BE (FE FF BOM) text frame (e.g. Amazon downloads) must remain
// readable after we rewrite it. saveMP3UTF8 transcodes such frames to UTF-8.
func TestUTF16MP3SurvivesWrite(t *testing.T) {
	// Minimal ID3v2.3 tag: one TIT2 frame, UTF-16BE with FE FF BOM, text "Hi".
	body := []byte{0x01, 0xFE, 0xFF, 0x00, 'H', 0x00, 'i', 0x00, 0x00}
	frame := append([]byte("TIT2"), 0, 0, 0, byte(len(body)), 0, 0)
	frame = append(frame, body...)
	tag := append([]byte("ID3"), 0x03, 0x00, 0x00, 0, 0, 0, byte(len(frame)))
	tag = append(tag, frame...)

	path := filepath.Join(t.TempDir(), "utf16.mp3")
	if err := os.WriteFile(path, tag, 0o644); err != nil {
		t.Fatal(err)
	}
	// Any write path that calls saveMP3UTF8 must leave it dhowden-readable.
	if err := writeReplayGainTags(path, -6, 0.9, -6, 0.9); err != nil {
		t.Fatalf("writeReplayGainTags: %v", err)
	}
	tt, err := readTrackTags(path)
	if err != nil {
		t.Fatalf("dhowden could not read after write (UTF-16 corruption): %v", err)
	}
	if tt.Title != "Hi" {
		t.Errorf("title not preserved: got %q", tt.Title)
	}
}

func TestRoundTripMP3(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.mp3")
	if err := os.WriteFile(path, nil, 0o644); err != nil { // id3v2 creates the tag on a tagless file
		t.Fatal(err)
	}
	roundTrip(t, path, tinyPNG(t))
}

func TestRoundTripFLAC(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.flac")
	writeMinimalFLAC(t, path)
	roundTrip(t, path, tinyPNG(t))
}

// TestWriteEmptyClearsFields verifies that blank text values remove frames
// rather than writing empty ones. It inspects the file with the id3v2 reader
// directly (a real MP3 would retain audio after a clear, but the synthetic
// fixture has none, so dhowden can't re-read an audio-less file).
func TestWriteEmptyClearsFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.mp3")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeTrackTags(path, trackTags{Title: "x", Year: 2000, Art: tinyPNG(t), ArtMIME: "image/png"}); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := writeTrackTags(path, trackTags{}); err != nil { // clear everything
		t.Fatalf("clear write: %v", err)
	}

	tag, err := id3v2.Open(path, id3v2.Options{Parse: true})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer tag.Close()
	if tag.Title() != "" {
		t.Errorf("title not cleared: %q", tag.Title())
	}
	if frames := tag.GetFrames(tag.CommonID("Attached picture")); len(frames) != 0 {
		t.Errorf("expected no APIC frames, got %d", len(frames))
	}
}
