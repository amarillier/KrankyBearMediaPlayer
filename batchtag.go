// Package main - batchtag.go is the batch tag editor: it applies a chosen set of
// tag fields to every marked track at once (e.g. set Album/Album Artist/Genre/
// Year across a whole album). Only the fields you tick are changed; each track's
// other tags - including its title, track number, and embedded cover - are read
// and rewritten untouched. It reuses the single-file writers (tagedit.go) and
// the Copy Selected progress/cancel pattern. MP3 and FLAC are writable; other
// formats in the selection are skipped.
package main

import (
	"fmt"
	"log"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// batchTarget is one track to update: its catalog id and on-disk path. Built
// from the marked set, so it covers all marked tracks regardless of the current
// filter/sort.
type batchTarget struct {
	id   int64
	path string
}

// tagPatch is the set of fields to apply across the selection. A set* flag means
// "change this field to the paired value"; unflagged fields are left as-is.
type tagPatch struct {
	setArtist, setAlbum, setAlbumArtist, setGenre, setComment, setYear bool
	artist, album, albumArtist, genre, comment                         string
	year                                                               int
}

func (p tagPatch) any() bool {
	return p.setArtist || p.setAlbum || p.setAlbumArtist || p.setGenre || p.setComment || p.setYear
}

// applyTo overwrites the flagged fields on tt, leaving the rest as they were read.
func (p tagPatch) applyTo(tt *trackTags) {
	if p.setArtist {
		tt.Artist = p.artist
	}
	if p.setAlbum {
		tt.Album = p.album
	}
	if p.setAlbumArtist {
		tt.AlbumArtist = p.albumArtist
	}
	if p.setGenre {
		tt.Genre = p.genre
	}
	if p.setComment {
		tt.Comment = p.comment
	}
	if p.setYear {
		tt.Year = p.year
	}
}

// editTagsOfSelected opens the batch tag editor for the marked tracks.
func (u *ui) editTagsOfSelected() {
	if len(u.marked) == 0 {
		dialog.ShowInformation("Edit tags of selected",
			"No tracks are marked. Turn on View → Selection checkboxes and tick some "+
				"tracks (or Library → Select All Shown), then try again.", u.win)
		return
	}
	targets := make([]batchTarget, 0, len(u.marked))
	for id, e := range u.marked {
		targets = append(targets, batchTarget{id: id, path: e.path})
	}

	// Each field is an entry that's disabled until its "Change" box is ticked, so
	// only the fields you mean to set get written.
	artist := widget.NewEntry()
	album := widget.NewEntry()
	albumArtist := widget.NewEntry()
	genre := widget.NewEntry()
	year := numericEntry(0)
	comment := widget.NewMultiLineEntry()
	comment.SetMinRowsVisible(2)

	form := widget.NewForm()
	checks := map[*widget.Entry]*widget.Check{}
	addField := func(label string, e *widget.Entry) {
		e.Disable()
		chk := widget.NewCheck("Change", func(on bool) {
			if on {
				e.Enable()
			} else {
				e.Disable()
			}
		})
		checks[e] = chk
		form.Append(label, container.NewBorder(nil, nil, chk, nil, e))
	}
	addField("Artist", artist)
	addField("Album", album)
	addField("Album Artist", albumArtist)
	addField("Genre", genre)
	addField("Year", year)
	addField("Comment", comment)

	note := widget.NewLabel(fmt.Sprintf(
		"Apply to %d marked track(s). Only ticked fields change; titles, track "+
			"numbers and cover art are left as-is. MP3 and FLAC only — other formats "+
			"are skipped.", len(targets)))
	note.Wrapping = fyne.TextWrapWord

	body := container.NewVScroll(container.NewVBox(note, form))
	d := dialog.NewCustomConfirm("Edit tags of selected", "Apply", "Cancel", body, func(ok bool) {
		if !ok {
			return
		}
		patch := tagPatch{
			setArtist:      checks[artist].Checked,
			setAlbum:       checks[album].Checked,
			setAlbumArtist: checks[albumArtist].Checked,
			setGenre:       checks[genre].Checked,
			setYear:        checks[year].Checked,
			setComment:     checks[comment].Checked,
			artist:         strings.TrimSpace(artist.Text),
			album:          strings.TrimSpace(album.Text),
			albumArtist:    strings.TrimSpace(albumArtist.Text),
			genre:          strings.TrimSpace(genre.Text),
			comment:        comment.Text,
		}
		patch.year, _ = strconv.Atoi(year.Text)
		if !patch.any() {
			dialog.ShowInformation("Edit tags of selected",
				"Tick the box next to at least one field to change.", u.win)
			return
		}
		u.applyTagPatch(targets, patch)
	}, u.win)
	d.Resize(fyne.NewSize(520, 560))
	d.Show()
}

// applyTagPatch writes the patch to every target on a background goroutine,
// showing a progress dialog with a Cancel button (mirrors copyEntriesTo). Each
// file's existing tags are read and merged so unticked fields survive. The
// catalog row is updated off the UI thread; the in-memory track + table refresh
// happen on it.
func (u *ui) applyTagPatch(targets []batchTarget, patch tagPatch) {
	// Stop playback if one of the targets is the currently-playing track - an open
	// handle blocks the rewrite on Windows (same reason as the single-file editor).
	if cur, ok := u.player.Current(); ok {
		for _, t := range targets {
			if t.id == cur.ID {
				u.player.Stop()
				u.status.SetText("Stopped playback to update tags")
				break
			}
		}
	}

	total := len(targets)
	statusLbl := widget.NewLabel(fmt.Sprintf("Updating %d file(s)…", total))
	statusLbl.Truncation = fyne.TextTruncateEllipsis
	prog := widget.NewProgressBar()
	prog.Max = float64(total)
	var canceled atomic.Bool
	cancelBtn := widget.NewButton("Cancel", func() { canceled.Store(true) })
	cancelBtn.Importance = widget.DangerImportance
	d := dialog.NewCustomWithoutButtons("Updating tags", container.NewVBox(
		statusLbl, prog, container.NewCenter(cancelBtn),
	), u.win)
	d.Resize(fyne.NewSize(420, 150))
	d.Show()

	go func() {
		var updated, skipped, failed int
		for i, t := range targets {
			if canceled.Load() {
				break
			}
			n, name := i+1, filepath.Base(t.path)
			fyne.Do(func() { statusLbl.SetText(fmt.Sprintf("Updating %d of %d: %s", n, total, name)) })

			if !tagsWritable(t.path) {
				skipped++
				fyne.Do(func() { prog.SetValue(float64(n)) })
				continue
			}
			tt, err := readTrackTags(t.path)
			if err != nil {
				log.Printf("batch tags: read %q: %v", t.path, err)
				failed++
				fyne.Do(func() { prog.SetValue(float64(n)) })
				continue
			}
			patch.applyTo(&tt)
			if err := writeTrackTags(t.path, tt); err != nil {
				log.Printf("batch tags: write %q: %v", t.path, err)
				failed++
				fyne.Do(func() { prog.SetValue(float64(n)) })
				continue
			}
			if err := u.db.UpdateTrackTags(t.id, tt); err != nil { // off-UI DB write is fine
				log.Printf("batch tags: catalog update %d: %v", t.id, err)
			}
			updated++

			id, merged := t.id, tt
			fyne.Do(func() {
				for j := range u.tracks {
					if u.tracks[j].ID == id {
						u.tracks[j].Title = merged.Title
						u.tracks[j].Artist = merged.Artist
						u.tracks[j].Album = merged.Album
						u.tracks[j].AlbumArtist = merged.AlbumArtist
						u.tracks[j].Genre = merged.Genre
						u.tracks[j].Year = merged.Year
						u.tracks[j].TrackNo = merged.Track
						break
					}
				}
				prog.SetValue(float64(n))
			})
		}

		wasCanceled := canceled.Load()
		fyne.Do(func() {
			d.Hide()
			u.table.Refresh()
			u.refreshNowPlaying()
			summary := fmt.Sprintf("Updated tags on %d file(s)", updated)
			if skipped > 0 {
				summary += fmt.Sprintf("; %d skipped (OGG/WAV not writable)", skipped)
			}
			if failed > 0 {
				summary += fmt.Sprintf("; %d failed (see log)", failed)
			}
			title := "Tags updated"
			if wasCanceled {
				title = "Tag update cancelled"
				summary = "Cancelled before finishing. " + summary
			}
			u.status.SetText(summary)
			dialog.ShowInformation(title, summary, u.win)
		})
	}()
}
