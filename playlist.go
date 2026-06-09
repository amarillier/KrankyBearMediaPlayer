// Package main - playlist.go handles static playlists saved as portable .m3u8
// files. Paths are written relative to the playlist file's own location, so a
// playlist and its music travel together (e.g. on a USB stick) - matching the
// app's relative-path library philosophy. Dynamic/smart playlists (saved filter
// criteria) live separately in the catalog DB; see smartplaylist.go.
package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
)

// writePlaylistM3U writes tracks as an extended M3U. Each entry gets an #EXTINF
// line (duration + "Artist - Title") followed by the file path relative to
// baseDir (the playlist file's directory), forward-slashed per M3U convention.
// When a relative path can't be formed (e.g. a different Windows volume), the
// absolute path is written instead so the entry still resolves.
func writePlaylistM3U(w io.Writer, baseDir string, tracks []Track) error {
	bw := bufio.NewWriter(w)
	if _, err := fmt.Fprintln(bw, "#EXTM3U"); err != nil {
		return err
	}
	for _, t := range tracks {
		title := t.Title
		if title == "" {
			title = filepath.Base(t.RelPath)
		}
		label := title
		if t.Artist != "" {
			label = t.Artist + " - " + title
		}
		fmt.Fprintf(bw, "#EXTINF:%d,%s\n", t.Duration, label)

		abs := t.AbsPath()
		entry := abs
		if rel, err := filepath.Rel(baseDir, abs); err == nil && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			entry = rel
		}
		fmt.Fprintln(bw, filepath.ToSlash(entry))
	}
	return bw.Flush()
}

// parsePlaylistM3U reads an M3U/M3U8 stream and returns the absolute file paths
// of its entries, resolving relative paths against baseDir. #EXTINF and other
// "#" comment lines (and blanks) are skipped.
func parsePlaylistM3U(r io.Reader, baseDir string) []string {
	var paths []string
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20) // tolerate long path lines
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		p := filepath.FromSlash(line)
		if !filepath.IsAbs(p) {
			p = filepath.Join(baseDir, p)
		}
		paths = append(paths, filepath.Clean(p))
	}
	return paths
}

// savePlaylist writes the current view (all shown tracks) to a .m3u8 file the
// user chooses. Filtering first (e.g. to 5 stars) then saving is the intended
// workflow, mirroring Copy Selected.
func (u *ui) savePlaylist() {
	if len(u.tracks) == 0 {
		dialog.ShowInformation("Save playlist", "No tracks are shown to save.", u.win)
		return
	}
	tracks := append([]Track(nil), u.tracks...) // snapshot; the save runs in a callback
	save := dialog.NewFileSave(func(wc fyne.URIWriteCloser, err error) {
		if err != nil || wc == nil {
			return
		}
		defer wc.Close()
		baseDir := filepath.Dir(wc.URI().Path())
		if err := writePlaylistM3U(wc, baseDir, tracks); err != nil {
			dialog.ShowError(err, u.win)
			return
		}
		u.status.SetText(fmt.Sprintf("Saved %d track(s) to %s", len(tracks), filepath.Base(wc.URI().Path())))
	}, u.win)
	save.SetFileName("playlist.m3u8")
	save.SetFilter(storage.NewExtensionFileFilter([]string{".m3u8", ".m3u"}))
	save.Show()
}

// openPlaylist loads a .m3u8 file and plays it. Entries are matched to catalogued
// tracks by path (to recover title/artist/rating/play count); files not in the
// catalog still play, labelled by filename.
func (u *ui) openPlaylist() {
	open := dialog.NewFileOpen(func(rc fyne.URIReadCloser, err error) {
		if err != nil || rc == nil {
			return
		}
		defer rc.Close()
		baseDir := filepath.Dir(rc.URI().Path())
		paths := parsePlaylistM3U(rc, baseDir)
		if len(paths) == 0 {
			dialog.ShowInformation("Open playlist", "The playlist is empty or unreadable.", u.win)
			return
		}
		tracks, missing := u.tracksForPaths(paths)
		if len(tracks) == 0 {
			dialog.ShowInformation("Open playlist", "None of the playlist's files could be found.", u.win)
			return
		}
		u.player.PlayQueue(tracks, 0)
		msg := fmt.Sprintf("Playing %d track(s) from %s", len(tracks), filepath.Base(rc.URI().Path()))
		if missing > 0 {
			msg += fmt.Sprintf(" (%d file(s) not found, skipped)", missing)
		}
		u.status.SetText(msg)
	}, u.win)
	open.SetFilter(storage.NewExtensionFileFilter([]string{".m3u8", ".m3u"}))
	open.Show()
}

// tracksForPaths maps absolute file paths to playable Tracks. Catalogued files
// are returned with their full metadata; non-catalogued files that exist on disk
// are returned as minimal tracks (so the playlist still plays). Returns the
// matched tracks (in playlist order) and the count of entries skipped as missing.
func (u *ui) tracksForPaths(paths []string) (tracks []Track, missing int) {
	all, err := u.db.Tracks(TrackQuery{Filter: FilterAll, SortCol: -1})
	if err != nil {
		log.Printf("playlist: load catalog: %v", err)
	}
	byPath := make(map[string]Track, len(all))
	for _, t := range all {
		byPath[filepath.Clean(t.AbsPath())] = t
	}
	for _, p := range paths {
		if t, ok := byPath[p]; ok {
			tracks = append(tracks, t)
			continue
		}
		if checkFileExists(p) {
			tracks = append(tracks, Track{RelPath: filepath.ToSlash(p), Title: filepath.Base(p)})
			continue
		}
		missing++
	}
	return tracks, missing
}
