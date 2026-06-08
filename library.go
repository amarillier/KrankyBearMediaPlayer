// Package main - library.go scans watched folders, reads tags + embedded
// album art with dhowden/tag (pure Go), and writes results to the catalog.
//
// Paths are stored relative to the folder root (see db.go) for portability.
package main

import (
	"bytes"
	"image"
	_ "image/gif"  // register GIF decoder for embedded art
	_ "image/jpeg" // register JPEG decoder for embedded art (most common)
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/dhowden/tag"
	xdraw "golang.org/x/image/draw"
)

// supportedExts are the audio formats the player can decode (see player.go).
var supportedExts = map[string]bool{
	".mp3":  true,
	".flac": true,
	".wav":  true,
	".ogg":  true,
}

const thumbSize = 64 // album-art thumbnail edge, pixels

// ScanResult summarizes one scan pass.
type ScanResult struct {
	Found   int // audio files seen
	Updated int // rows inserted/updated
	Errors  int // files that failed to read
}

// ScanFolder walks one watched folder, cataloguing every supported audio file.
// progress, if non-nil, is called with the current file path as it goes.
func (d *DB) ScanFolder(f Folder, progress func(path string)) (ScanResult, error) {
	var res ScanResult
	root := f.Path

	walkFn := func(path string, info os.DirEntry, err error) error {
		if err != nil {
			res.Errors++
			return nil // skip unreadable entries, keep going
		}
		if info.IsDir() {
			if !f.Recursive && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !supportedExts[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		res.Found++
		if progress != nil {
			progress(path)
		}
		if err := d.catalogFile(f.ID, root, path); err != nil {
			res.Errors++
			return nil
		}
		res.Updated++
		return nil
	}

	if err := filepath.WalkDir(root, walkFn); err != nil {
		return res, err
	}
	_ = d.SetFolderScanned(f.ID)
	return res, nil
}

// catalogFile reads one file's metadata + art and upserts it.
func (d *DB) catalogFile(folderID int64, root, absPath string) error {
	rel, err := filepath.Rel(root, absPath)
	if err != nil {
		return err
	}
	rel = filepath.ToSlash(rel) // normalize so it survives moving across OSes

	fi, err := os.Stat(absPath)
	if err != nil {
		return err
	}

	t := &Track{
		FolderID:  folderID,
		RelPath:   rel,
		FileSize:  fi.Size(),
		FileMtime: fi.ModTime().Unix(),
		// Fallback title is the filename without extension.
		Title: strings.TrimSuffix(filepath.Base(absPath), filepath.Ext(absPath)),
	}

	var pic *tag.Picture
	if f, err := os.Open(absPath); err == nil {
		if m, err := tag.ReadFrom(f); err == nil {
			if v := m.Title(); v != "" {
				t.Title = v
			}
			t.Artist = m.Artist()
			t.Album = m.Album()
			t.AlbumArtist = m.AlbumArtist()
			t.Genre = m.Genre()
			t.Year = m.Year()
			t.TrackNo, _ = m.Track()
			pic = m.Picture()
			t.HasArt = pic != nil
		}
		f.Close()
	}

	if err := d.UpsertTrack(t); err != nil {
		return err
	}

	// Store a small thumbnail if the file had embedded art.
	if pic != nil && t.ID != 0 {
		if thumb, err := makeThumbnail(pic.Data, thumbSize); err == nil {
			_ = d.SetThumb(t.ID, thumb)
		}
	}
	return nil
}

// makeThumbnail decodes arbitrary image bytes (JPEG/PNG/etc. from embedded art)
// and re-encodes a square-ish PNG thumbnail scaled to fit maxEdge. We use the
// standard library + x/image/draw (already a dependency) to avoid new deps.
func makeThumbnail(data []byte, maxEdge int) ([]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return nil, err
	}
	// Scale longest edge to maxEdge, preserving aspect ratio.
	scale := float64(maxEdge) / float64(max(w, h))
	nw, nh := int(float64(w)*scale), int(float64(h)*scale)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Over, nil)

	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
