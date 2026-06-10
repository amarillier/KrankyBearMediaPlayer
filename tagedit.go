// Package main - tagedit.go reads and writes a track's editable metadata
// (text tags + one embedded front-cover image). Reading is uniform via
// dhowden/tag (read-only, same as the library scanner); writing is per-format
// and pure-Go: ID3v2 for MP3 (bogem/id3v2) and Vorbis comments + a PICTURE
// block for FLAC (go-flac). OGG and WAV are read-only for now - tagsWritable
// reports false and the UI opens them in a disabled, view-only state.
package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bogem/id3v2/v2"
	"github.com/dhowden/tag"
	flacpicture "github.com/go-flac/flacpicture"
	flacvorbis "github.com/go-flac/flacvorbis"
	flac "github.com/go-flac/go-flac"
)

// trackTags is the set of fields the editor exposes. Art holds the raw bytes of
// the embedded front cover (nil = none); ArtMIME is its content type.
type trackTags struct {
	Title       string
	Artist      string
	Album       string
	AlbumArtist string
	Genre       string
	Comment     string
	Year        int
	Track       int
	Art         []byte
	ArtMIME     string
}

// tagsWritable reports whether writeTrackTags can save changes for this file's
// format. Reading works for every format the player supports; only MP3 and FLAC
// can be written back (pure Go).
func tagsWritable(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3", ".flac":
		return true
	}
	return false
}

// readTrackTags reads the current metadata + embedded cover from a file. A file
// with no tags at all is not an error - it yields zero-value fields so the
// editor can populate them.
func readTrackTags(path string) (trackTags, error) {
	f, err := os.Open(path)
	if err != nil {
		return trackTags{}, err
	}
	defer f.Close()

	m, err := tag.ReadFrom(f)
	if err == tag.ErrNoTagsFound {
		return trackTags{}, nil
	}
	if err != nil {
		return trackTags{}, err
	}

	tt := trackTags{
		Title:       m.Title(),
		Artist:      m.Artist(),
		Album:       m.Album(),
		AlbumArtist: m.AlbumArtist(),
		Genre:       m.Genre(),
		Comment:     m.Comment(),
		Year:        m.Year(),
	}
	tt.Track, _ = m.Track()
	if p := m.Picture(); p != nil {
		tt.Art = p.Data
		tt.ArtMIME = p.MIMEType
	}
	return tt, nil
}

// writeTrackTags writes the edited metadata back to the file, dispatching by
// extension. Callers must ensure the file isn't held open elsewhere (e.g. the
// player) - on Windows an open handle blocks the rewrite.
func writeTrackTags(path string, tt trackTags) error {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		return writeMP3Tags(path, tt)
	case ".flac":
		return writeFLACTags(path, tt)
	}
	return fmt.Errorf("editing %s tags isn't supported yet", filepath.Ext(path))
}

// detectImageMIME returns the image content type for raw bytes (e.g.
// "image/jpeg"), falling back to "image/jpeg" when undetected.
func detectImageMIME(data []byte) string {
	mime := http.DetectContentType(data)
	if strings.HasPrefix(mime, "image/") {
		return mime
	}
	return "image/jpeg"
}

// writeMP3Tags writes ID3v2 frames. Text frames are replaced in place; the
// in-sequence COMM/APIC frames are cleared first to avoid duplicates. Empty
// text values delete the corresponding frame rather than writing a blank one.
func writeMP3Tags(path string, tt trackTags) error {
	t, err := id3v2.Open(path, id3v2.Options{Parse: true})
	if err != nil {
		return err
	}
	defer t.Close()

	enc := id3v2.EncodingUTF8
	t.SetDefaultEncoding(enc)

	setText := func(commonName, value string) {
		id := t.CommonID(commonName)
		if value == "" {
			t.DeleteFrames(id)
			return
		}
		t.AddTextFrame(id, enc, value)
	}
	setText("Title", tt.Title)
	setText("Artist", tt.Artist)
	setText("Album/Movie/Show title", tt.Album)
	setText("Band/Orchestra/Accompaniment", tt.AlbumArtist) // TPE2
	setText("Content type", tt.Genre)                       // TCON

	if tt.Track > 0 {
		t.AddTextFrame(t.CommonID("Track number/Position in set"), enc, strconv.Itoa(tt.Track))
	} else {
		t.DeleteFrames(t.CommonID("Track number/Position in set"))
	}

	// Year frame ID differs between ID3v2.3 (TYER) and 2.4 (TDRC); clear both.
	t.DeleteFrames("TYER")
	t.DeleteFrames("TDRC")
	if tt.Year > 0 {
		t.SetYear(strconv.Itoa(tt.Year))
	}

	comm := t.CommonID("Comments")
	t.DeleteFrames(comm)
	if tt.Comment != "" {
		t.AddCommentFrame(id3v2.CommentFrame{Encoding: enc, Language: "eng", Text: tt.Comment})
	}

	pic := t.CommonID("Attached picture")
	t.DeleteFrames(pic)
	if len(tt.Art) > 0 {
		t.AddAttachedPicture(id3v2.PictureFrame{
			Encoding:    enc,
			MimeType:    tt.ArtMIME,
			PictureType: id3v2.PTFrontCover,
			Description: "",
			Picture:     tt.Art,
		})
	}

	return t.Save()
}

// writeFLACTags rebuilds the file's VORBIS_COMMENT block from the edited fields
// and the PICTURE block from the cover, preserving all other metadata blocks
// (STREAMINFO, etc.). go-flac rewrites the whole file on Save.
func writeFLACTags(path string, tt trackTags) error {
	f, err := flac.ParseFile(path)
	if err != nil {
		return err
	}

	cmt := flacvorbis.New()
	add := func(field, value string) {
		if value != "" {
			_ = cmt.Add(field, value)
		}
	}
	add(flacvorbis.FIELD_TITLE, tt.Title)
	add(flacvorbis.FIELD_ARTIST, tt.Artist)
	add(flacvorbis.FIELD_ALBUM, tt.Album)
	add("ALBUMARTIST", tt.AlbumArtist)
	add(flacvorbis.FIELD_GENRE, tt.Genre)
	add(flacvorbis.FIELD_DESCRIPTION, tt.Comment)
	if tt.Year > 0 {
		add(flacvorbis.FIELD_DATE, strconv.Itoa(tt.Year))
	}
	if tt.Track > 0 {
		add(flacvorbis.FIELD_TRACKNUMBER, strconv.Itoa(tt.Track))
	}
	cmtBlock := cmt.Marshal()

	// Keep every block except the ones we regenerate (comment + pictures).
	kept := f.Meta[:0]
	for _, b := range f.Meta {
		if b.Type == flac.VorbisComment || b.Type == flac.Picture {
			continue
		}
		kept = append(kept, b)
	}
	f.Meta = append(kept, &cmtBlock)

	if len(tt.Art) > 0 {
		p, err := flacpicture.NewFromImageData(flacpicture.PictureTypeFrontCover, "", tt.Art, tt.ArtMIME)
		if err != nil {
			return fmt.Errorf("cover image: %w", err)
		}
		picBlock := p.Marshal()
		f.Meta = append(f.Meta, &picBlock)
	}

	return f.Save(path)
}
