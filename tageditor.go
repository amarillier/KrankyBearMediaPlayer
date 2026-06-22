// Package main - tageditor.go is the "Edit tags…" dialog: it reads a track's
// current metadata + embedded cover (tagedit.go), lets the user change them,
// writes them back to the file, and reconciles the catalog row without a
// destructive re-scan. MP3 and FLAC are editable; other formats open read-only.
package main

import (
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"mediaplayer/internal/i18n"
)

// numericEntry is a single-line entry that silently drops non-digit input, so
// the Year and Track fields always parse cleanly (empty == 0).
func numericEntry(value int) *widget.Entry {
	e := widget.NewEntry()
	if value > 0 {
		e.SetText(strconv.Itoa(value))
	}
	e.OnChanged = func(s string) {
		clean := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, s)
		if clean != s {
			e.SetText(clean)
		}
	}
	return e
}

// editTags opens the tag editor for the given row.
func (u *ui) editTags(row int) {
	if row < 0 || row >= len(u.tracks) {
		return
	}
	tr := u.tracks[row]
	path := tr.AbsPath()

	tt, err := readTrackTags(path)
	if err != nil {
		dialog.ShowError(fmt.Errorf("could not read tags from %s: %w", tr.RelPath, err), u.win)
		return
	}
	writable := tagsWritable(path)

	// --- text fields ---
	title := widget.NewEntry()
	title.SetText(tt.Title)
	artist := widget.NewEntry()
	artist.SetText(tt.Artist)
	album := widget.NewEntry()
	album.SetText(tt.Album)
	albumArtist := widget.NewEntry()
	albumArtist.SetText(tt.AlbumArtist)
	genre := widget.NewEntry()
	genre.SetText(tt.Genre)
	year := numericEntry(tt.Year)
	track := numericEntry(tt.Track)
	comment := widget.NewMultiLineEntry()
	comment.SetText(tt.Comment)
	comment.Wrapping = fyne.TextWrapWord
	comment.SetMinRowsVisible(2)

	// --- embedded cover ---
	// curArt/curMIME hold the cover to write; artDirty marks a user change so we
	// only touch the catalog thumbnail when the cover actually changed.
	curArt, curMIME := tt.Art, tt.ArtMIME
	artDirty := false
	preview := canvas.NewImageFromResource(nil)
	preview.FillMode = canvas.ImageFillContain
	preview.SetMinSize(fyne.NewSize(140, 140))
	noArt := widget.NewLabel(i18n.T("tageditor.no_cover"))
	refreshPreview := func() {
		if len(curArt) > 0 {
			preview.Resource = fyne.NewStaticResource("cover", curArt)
			preview.Refresh()
			preview.Show()
			noArt.Hide()
		} else {
			preview.Hide()
			noArt.Show()
		}
	}
	refreshPreview()

	changeArt := widget.NewButtonWithIcon(i18n.T("tageditor.change_art"), theme.FolderOpenIcon(), func() {
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
			artDirty = true
			refreshPreview()
		}, u.win)
		fd.SetFilter(storage.NewExtensionFileFilter([]string{".png", ".jpg", ".jpeg", ".gif"}))
		fd.Show()
	})
	removeArt := widget.NewButtonWithIcon(i18n.T("common.remove"), theme.DeleteIcon(), func() {
		curArt, curMIME = nil, ""
		artDirty = true
		refreshPreview()
	})
	artButtons := container.NewVBox(changeArt, removeArt, noArt)
	artRow := container.NewBorder(nil, nil, container.NewGridWrap(fyne.NewSize(150, 150), preview), nil, artButtons)

	form := widget.NewForm(
		widget.NewFormItem(i18n.T("tag.title"), title),
		widget.NewFormItem(i18n.T("tag.artist"), artist),
		widget.NewFormItem(i18n.T("tag.album"), album),
		widget.NewFormItem(i18n.T("tag.album_artist"), albumArtist),
		widget.NewFormItem(i18n.T("tag.year"), year),
		widget.NewFormItem(i18n.T("tag.track"), track),
		widget.NewFormItem(i18n.T("tag.genre"), genre),
		widget.NewFormItem(i18n.T("tag.comment"), comment),
		widget.NewFormItem(i18n.T("tag.cover"), artRow),
	)

	if !writable {
		// Read-only: show the values but disable editing and offer no Save.
		form.Disable()
		changeArt.Disable()
		removeArt.Disable()
		banner := widget.NewLabelWithStyle(
			i18n.T("tageditor.readonly_note"),
			fyne.TextAlignLeading, fyne.TextStyle{Italic: true})
		content := container.NewBorder(banner, nil, nil, nil, container.NewVScroll(form))
		d := dialog.NewCustom(i18n.TC("tageditor.readonly_title", map[string]string{"file": tr.RelPath}), i18n.T("common.close"), content, u.win)
		d.Resize(fyne.NewSize(560, 620))
		d.Show()
		return
	}

	// "Tags from filename…" parses the on-disk name into these fields for review
	// (it populates the entries; it never saves on its own).
	entries := map[string]*widget.Entry{
		"title": title, "artist": artist, "album": album,
		"albumartist": albumArtist, "genre": genre, "year": year, "track": track,
	}
	fromName := widget.NewButtonWithIcon(i18n.T("tageditor.from_filename"), theme.SearchIcon(), func() {
		u.tagsFromFilename(path, entries)
	})

	content := container.NewBorder(container.NewHBox(fromName), nil, nil, nil, container.NewVScroll(form))
	d := dialog.NewCustomConfirm(i18n.TC("tageditor.edit_title", map[string]string{"file": tr.RelPath}), i18n.T("common.save"), i18n.T("common.cancel"), content, func(ok bool) {
		if !ok {
			return
		}
		edited := trackTags{
			Title:       strings.TrimSpace(title.Text),
			Artist:      strings.TrimSpace(artist.Text),
			Album:       strings.TrimSpace(album.Text),
			AlbumArtist: strings.TrimSpace(albumArtist.Text),
			Genre:       strings.TrimSpace(genre.Text),
			Comment:     comment.Text,
			Art:         curArt,
			ArtMIME:     curMIME,
		}
		edited.Year, _ = strconv.Atoi(year.Text)
		edited.Track, _ = strconv.Atoi(track.Text)
		u.saveTags(tr, edited, artDirty)
	}, u.win)
	d.Resize(fyne.NewSize(560, 620))
	d.Show()
}

// saveTags writes the edited tags to the file and reconciles the catalog. If the
// track is currently loaded in the player, playback is stopped first so the file
// handle is released (a write would otherwise fail on Windows). The file write
// runs off the UI goroutine since FLAC rewrites the whole file.
func (u *ui) saveTags(tr Track, edited trackTags, artDirty bool) {
	if cur, ok := u.player.Current(); ok && cur.ID == tr.ID {
		u.player.Stop()
		u.status.SetText(i18n.T("status.stopped_save_tags"))
	}
	path := tr.AbsPath()
	u.status.SetText(i18n.T("status.saving_tags"))
	go func() {
		err := writeTrackTags(path, edited)
		fyne.Do(func() {
			if err != nil {
				dialog.ShowError(fmt.Errorf("could not save tags: %w", err), u.win)
				u.status.SetText("")
				return
			}
			u.applyEditedTags(tr.ID, edited, artDirty)
		})
	}()
}

// applyEditedTags updates the catalog row + in-memory track from a successful
// write and refreshes the view. Runs on the UI goroutine.
func (u *ui) applyEditedTags(trackID int64, edited trackTags, artDirty bool) {
	if err := u.db.UpdateTrackTags(trackID, edited); err != nil {
		dialog.ShowError(err, u.win)
		return
	}
	if artDirty {
		if len(edited.Art) > 0 {
			if png, err := makeThumbnail(edited.Art, thumbSize); err == nil {
				_ = u.db.SetArt(trackID, png)
			}
		} else {
			_ = u.db.ClearArt(trackID)
		}
		delete(u.thumbCache, trackID)
	}

	for i := range u.tracks {
		if u.tracks[i].ID == trackID {
			u.tracks[i].Title = edited.Title
			u.tracks[i].Artist = edited.Artist
			u.tracks[i].Album = edited.Album
			u.tracks[i].AlbumArtist = edited.AlbumArtist
			u.tracks[i].Genre = edited.Genre
			u.tracks[i].Year = edited.Year
			u.tracks[i].TrackNo = edited.Track
			if artDirty {
				u.tracks[i].HasArt = len(edited.Art) > 0
			}
			break
		}
	}
	u.table.Refresh()
	u.refreshNowPlaying()
	u.status.SetText(i18n.T("status.tags_saved"))
}

// parseFieldOrder is the display/apply order for parsed filename fields. The label
// is the English fallback; the live label comes from tagFieldLabelKey via i18n.
var parseFieldOrder = []struct{ key, label string }{
	{"track", "Track #"}, {"title", "Title"}, {"artist", "Artist"},
	{"album", "Album"}, {"albumartist", "Album Artist"},
	{"year", "Year"}, {"genre", "Genre"},
}

// tagFieldLabelKey maps a parsed-field key to its tag.* i18n key (shared with the
// editor's form labels). Returns "" for unknown keys (caller falls back to English).
func tagFieldLabelKey(key string) string {
	switch key {
	case "track":
		return "tag.track"
	case "title":
		return "tag.title"
	case "artist":
		return "tag.artist"
	case "album":
		return "tag.album"
	case "albumartist":
		return "tag.album_artist"
	case "year":
		return "tag.year"
	case "genre":
		return "tag.genre"
	}
	return ""
}

// tagsFromFilename opens the inverse-of-rename dialog: it parses the file's name into
// tag fields using the shared pattern grammar (pattern.go) and, on Apply, fills only
// the matched editor entries for the user to review and Save. It never writes to disk.
func (u *ui) tagsFromFilename(path string, entries map[string]*widget.Entry) {
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))

	leadChk := widget.NewCheck(i18n.T("tageditor.leading_track"), nil)
	preview := widget.NewLabel("")
	preview.Wrapping = fyne.TextWrapWord

	var patEntry *widget.Entry
	var presets *widget.Select
	update := func() {
		parsed, ok := parseName(patEntry.Text, stem, leadChk.Checked)
		if !ok {
			preview.SetText(i18n.T("tageditor.no_match_preview"))
			return
		}
		var b strings.Builder
		for _, f := range parseFieldOrder {
			if v, present := parsed[f.key]; present {
				label := f.label
				if k := tagFieldLabelKey(f.key); k != "" {
					label = i18n.T(k)
				}
				fmt.Fprintf(&b, "%s = %s\n", label, v)
			}
		}
		preview.SetText(strings.TrimRight(b.String(), "\n"))
	}
	patEntry, presets = u.patternPicker(prefParsePattern, renamePresets[1], update)
	leadChk.OnChanged = func(bool) { update() }
	update()

	help := widget.NewLabelWithStyle(
		i18n.TC("tageditor.source_tokens", map[string]string{"file": filepath.Base(path)}),
		fyne.TextAlignLeading, fyne.TextStyle{Italic: true})
	help.Wrapping = fyne.TextWrapWord

	form := widget.NewForm(
		widget.NewFormItem(i18n.T("tageditor.pattern_label"), patEntry),
		widget.NewFormItem("", presets),
		widget.NewFormItem("", leadChk),
		widget.NewFormItem(i18n.T("tageditor.will_set"), preview),
	)
	body := container.NewVBox(form, help)
	d := dialog.NewCustomConfirm(i18n.T("tageditor.from_title"), i18n.T("tageditor.apply_fields"), i18n.T("common.cancel"), body, func(ok bool) {
		if !ok {
			return
		}
		parsed, matched := parseName(patEntry.Text, stem, leadChk.Checked)
		if !matched {
			dialog.ShowInformation(i18n.T("tageditor.from_title"),
				i18n.T("tageditor.no_match"), u.win)
			return
		}
		u.app.Preferences().SetString(prefParsePattern, patEntry.Text)
		for key, val := range parsed {
			if e, has := entries[key]; has {
				e.SetText(val)
			}
		}
	}, u.win)
	d.Resize(fyne.NewSize(520, 320))
	d.Show()
}
