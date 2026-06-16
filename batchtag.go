// Package main - batchtag.go is the batch tag editor: it applies a chosen set of
// changes to every marked track at once. Three kinds of change can be combined in a
// single pass, applied in order:
//  1. From filename - parse each file's name with the shared pattern grammar
//     (pattern.go) and set the matched fields (title/artist/track/…) per file. This
//     is the bulk form of the editor's "Tags from filename".
//  2. Fixed fields - set Album/Album Artist/Genre/Year/Comment (and Artist) to one
//     value across the whole selection (the original batch behaviour).
//  3. Cover art - set one embedded cover image for all, or remove cover from all.
//
// Each track's untouched tags are read and rewritten as-is. It reuses the single-file
// writers (tagedit.go), the thumbnail/catalog reconcile from the single-file editor
// (tageditor.go), and the Copy Selected progress/cancel pattern. MP3 and FLAC are
// writable; other formats in the selection are skipped.
package main

import (
	"fmt"
	"io"
	"log"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Cover-art modes for a batch patch.
const (
	artLeave = iota // don't touch existing covers
	artSet          // set one chosen image on every track
	artClear        // remove the embedded cover from every track
)

// batchTarget is one track to update: its catalog id and on-disk path. Built
// from the marked set, so it covers all marked tracks regardless of the current
// filter/sort.
type batchTarget struct {
	id   int64
	path string
}

// tagPatch is the set of changes to apply across the selection. A set* flag means
// "change this field to the paired value"; unflagged fields are left as-is. usePattern
// derives title/artist/track/… per file from its name; artMode controls the cover.
type tagPatch struct {
	setArtist, setAlbum, setAlbumArtist, setGenre, setComment, setYear bool
	artist, album, albumArtist, genre, comment                         string
	year                                                               int

	usePattern   bool
	pattern      string
	leadingTrack bool

	frUse             bool   // find-and-replace within one field
	frField           string // "Title"/"Artist"/"Album"/"Album Artist"/"Genre"/"Comment"
	frFind, frReplace string
	frCI              bool // case-insensitive find

	artMode int // artLeave / artSet / artClear
	art     []byte
	artMIME string
}

func (p tagPatch) any() bool {
	return p.usePattern || p.artMode != artLeave || (p.frUse && p.frFind != "") ||
		p.setArtist || p.setAlbum || p.setAlbumArtist || p.setGenre || p.setComment || p.setYear
}

// frFields are the fields find-and-replace can target, in dialog order.
var frFields = []string{"Title", "Artist", "Album", "Album Artist", "Genre", "Comment"}

// replaceIn applies the patch's find/replace to s (case-sensitive or insensitive).
func (p tagPatch) replaceIn(s string) string {
	if p.frCI {
		re := regexp.MustCompile("(?i)" + regexp.QuoteMeta(p.frFind))
		return re.ReplaceAllString(s, p.frReplace)
	}
	return strings.ReplaceAll(s, p.frFind, p.frReplace)
}

// applyTo overwrites fields on tt, leaving the rest as they were read. Per-file
// pattern parsing (from the file at path) runs first; fixed-value fields then override
// it where ticked. Cover art is handled by the caller (it also drives the thumbnail).
func (p tagPatch) applyTo(tt *trackTags, path string) {
	if p.usePattern {
		stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if parsed, ok := parseName(p.pattern, stem, p.leadingTrack); ok {
			if v, has := parsed["title"]; has {
				tt.Title = v
			}
			if v, has := parsed["artist"]; has {
				tt.Artist = v
			}
			if v, has := parsed["album"]; has {
				tt.Album = v
			}
			if v, has := parsed["albumartist"]; has {
				tt.AlbumArtist = v
			}
			if v, has := parsed["genre"]; has {
				tt.Genre = v
			}
			if v, has := parsed["track"]; has {
				if n, err := strconv.Atoi(v); err == nil {
					tt.Track = n
				}
			}
			if v, has := parsed["year"]; has {
				if n, err := strconv.Atoi(v); err == nil {
					tt.Year = n
				}
			}
		}
	}
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
	// Find-and-replace runs last so it transforms the field's final value.
	if p.frUse && p.frFind != "" {
		switch p.frField {
		case "Title":
			tt.Title = p.replaceIn(tt.Title)
		case "Artist":
			tt.Artist = p.replaceIn(tt.Artist)
		case "Album":
			tt.Album = p.replaceIn(tt.Album)
		case "Album Artist":
			tt.AlbumArtist = p.replaceIn(tt.AlbumArtist)
		case "Genre":
			tt.Genre = p.replaceIn(tt.Genre)
		case "Comment":
			tt.Comment = p.replaceIn(tt.Comment)
		}
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

	// --- Section 1: from filename (per-file pattern) ---
	patUse := widget.NewCheck("Set tags from each file's name", nil)
	leadChk := widget.NewCheck("Filename has a leading track number", nil)
	patEntry, presets := u.patternPicker(prefParsePattern, renamePresets[1], func() {})
	patEntry.Disable()
	presets.Disable()
	leadChk.Disable()
	patUse.OnChanged = func(on bool) {
		if on {
			patEntry.Enable()
			presets.Enable()
			leadChk.Enable()
		} else {
			patEntry.Disable()
			presets.Disable()
			leadChk.Disable()
		}
	}
	patHelp := widget.NewLabelWithStyle(
		"Tokens: %track% %title% %artist% %album% %albumartist% %year% %genre%. "+
			"Files whose name doesn't match are left unchanged.",
		fyne.TextAlignLeading, fyne.TextStyle{Italic: true})
	patHelp.Wrapping = fyne.TextWrapWord
	patternSection := container.NewVBox(patUse, patEntry, presets, leadChk, patHelp)

	// --- Section 2: fixed fields (one value for all) ---
	artist := widget.NewEntry()
	album := widget.NewEntry()
	albumArtist := widget.NewEntry()
	genre := widget.NewEntry()
	year := numericEntry(0)
	comment := widget.NewMultiLineEntry()
	comment.SetMinRowsVisible(2)

	fixedForm := widget.NewForm()
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
		fixedForm.Append(label, container.NewBorder(nil, nil, chk, nil, e))
	}
	addField("Artist", artist)
	addField("Album", album)
	addField("Album Artist", albumArtist)
	addField("Genre", genre)
	addField("Year", year)
	addField("Comment", comment)

	// --- Section 3: cover art for all ---
	artMode := artLeave
	var curArt []byte
	var curMIME string
	preview := canvas.NewImageFromResource(nil)
	preview.FillMode = canvas.ImageFillContain
	preview.SetMinSize(fyne.NewSize(96, 96))
	preview.Hide()
	chooseBtn := widget.NewButtonWithIcon("Choose image…", theme.FolderOpenIcon(), nil)
	chooseBtn.Disable()
	chooseBtn.OnTapped = func() {
		fd := dialog.NewFileOpen(func(rc fyne.URIReadCloser, err error) {
			if err != nil || rc == nil {
				return
			}
			defer rc.Close()
			data, err := io.ReadAll(io.LimitReader(rc, maxArtBytes))
			if err != nil {
				dialog.ShowError(err, u.win)
				return
			}
			curArt, curMIME = data, detectImageMIME(data)
			preview.Resource = fyne.NewStaticResource("cover", curArt)
			preview.Refresh()
			preview.Show()
		}, u.win)
		fd.SetFilter(storage.NewExtensionFileFilter([]string{".png", ".jpg", ".jpeg", ".gif"}))
		fd.Show()
	}
	artRadio := widget.NewRadioGroup(
		[]string{"Leave covers as-is", "Set one image for all", "Remove cover from all"},
		func(s string) {
			switch s {
			case "Set one image for all":
				artMode = artSet
				chooseBtn.Enable()
			case "Remove cover from all":
				artMode = artClear
				chooseBtn.Disable()
			default:
				artMode = artLeave
				chooseBtn.Disable()
			}
		})
	artRadio.SetSelected("Leave covers as-is")
	coverSection := container.NewVBox(
		artRadio,
		container.NewBorder(nil, nil, container.NewGridWrap(fyne.NewSize(100, 100), preview), nil, chooseBtn),
	)

	// --- Section 4: find & replace within one field ---
	frUse := widget.NewCheck("Find & replace in a field", nil)
	frField := widget.NewSelect(frFields, nil)
	frField.SetSelectedIndex(0)
	frFind := widget.NewEntry()
	frFind.SetPlaceHolder("Find…")
	frReplace := widget.NewEntry()
	frReplace.SetPlaceHolder("Replace with…")
	frCI := widget.NewCheck("Ignore case", nil)
	frField.Disable()
	frFind.Disable()
	frReplace.Disable()
	frCI.Disable()
	frUse.OnChanged = func(on bool) {
		for _, w := range []fyne.Disableable{frField, frFind, frReplace, frCI} {
			if on {
				w.Enable()
			} else {
				w.Disable()
			}
		}
	}
	frSection := container.NewVBox(
		frUse,
		container.NewBorder(nil, nil, widget.NewLabel("Field"), nil, frField),
		frFind, frReplace, frCI,
	)

	sep := widget.NewSeparator
	note := widget.NewLabel(fmt.Sprintf(
		"Apply to %d marked track(s). MP3 and FLAC only — other formats are skipped.", len(targets)))
	note.Wrapping = fyne.TextWrapWord

	body := container.NewVScroll(container.NewVBox(
		note,
		widget.NewLabelWithStyle("From filename", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		patternSection,
		sep(),
		widget.NewLabelWithStyle("Set fields (same value for all)", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		fixedForm,
		sep(),
		widget.NewLabelWithStyle("Find & replace", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		frSection,
		sep(),
		widget.NewLabelWithStyle("Cover art", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		coverSection,
	))

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
			usePattern:     patUse.Checked,
			pattern:        strings.TrimSpace(patEntry.Text),
			leadingTrack:   leadChk.Checked,
			frUse:          frUse.Checked,
			frField:        frField.Selected,
			frFind:         frFind.Text,
			frReplace:      frReplace.Text,
			frCI:           frCI.Checked,
			artMode:        artMode,
			art:            curArt,
			artMIME:        curMIME,
		}
		patch.year, _ = strconv.Atoi(year.Text)

		if patch.frUse && patch.frFind == "" {
			dialog.ShowInformation("Edit tags of selected",
				"Enter the text to find, or untick \"Find & replace in a field\".", u.win)
			return
		}

		if patch.usePattern && patch.pattern == "" {
			dialog.ShowInformation("Edit tags of selected",
				"Enter a pattern, or untick \"Set tags from each file's name\".", u.win)
			return
		}
		if patch.artMode == artSet && len(patch.art) == 0 {
			dialog.ShowInformation("Edit tags of selected",
				"Choose an image for the cover, or pick a different cover option.", u.win)
			return
		}
		if !patch.any() {
			dialog.ShowInformation("Edit tags of selected",
				"Pick at least one change: a filename pattern, a ticked field, or a cover option.", u.win)
			return
		}
		if patch.usePattern {
			u.app.Preferences().SetString(prefParsePattern, patEntry.Text)
		}
		u.applyTagPatch(targets, patch)
	}, u.win)
	d.Resize(fyne.NewSize(560, 640))
	d.Show()
}

// applyTagPatch writes the patch to every target on a background goroutine,
// showing a progress dialog with a Cancel button (mirrors copyEntriesTo). Each
// file's existing tags are read and merged so untouched fields survive. The catalog
// row (and, when the cover changes, the thumbnail) is updated off the UI thread; the
// in-memory track + table refresh happen on it.
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
			patch.applyTo(&tt, t.path)
			switch patch.artMode {
			case artSet:
				tt.Art, tt.ArtMIME = patch.art, patch.artMIME
			case artClear:
				tt.Art, tt.ArtMIME = nil, ""
			}
			if err := writeTrackTags(t.path, tt); err != nil {
				log.Printf("batch tags: write %q: %v", t.path, err)
				failed++
				fyne.Do(func() { prog.SetValue(float64(n)) })
				continue
			}
			if err := u.db.UpdateTrackTags(t.id, tt); err != nil { // off-UI DB write is fine
				log.Printf("batch tags: catalog update %d: %v", t.id, err)
			}
			if patch.artMode != artLeave { // reconcile the cached thumbnail
				if patch.artMode == artSet && len(tt.Art) > 0 {
					if png, err := makeThumbnail(tt.Art, thumbSize); err == nil {
						_ = u.db.SetArt(t.id, png)
					}
				} else {
					_ = u.db.ClearArt(t.id)
				}
			}
			updated++

			id, merged, artChanged := t.id, tt, patch.artMode != artLeave
			fyne.Do(func() {
				if artChanged {
					delete(u.thumbCache, id)
				}
				for j := range u.tracks {
					if u.tracks[j].ID == id {
						u.tracks[j].Title = merged.Title
						u.tracks[j].Artist = merged.Artist
						u.tracks[j].Album = merged.Album
						u.tracks[j].AlbumArtist = merged.AlbumArtist
						u.tracks[j].Genre = merged.Genre
						u.tracks[j].Year = merged.Year
						u.tracks[j].TrackNo = merged.Track
						if artChanged {
							u.tracks[j].HasArt = len(merged.Art) > 0
						}
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
