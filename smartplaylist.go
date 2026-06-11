// Package main - smartplaylist.go is the UI for dynamic ("smart") playlists:
// named, saved filter criteria (rating + genre/artist/album/search) that
// re-evaluate live against the catalog whenever applied. Definitions are stored
// in the catalog DB (see SmartPlaylist in db.go); applying one drives the same
// library view + query path as the toolbar filter.
package main

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// clearSmartCriteria drops any active smart-playlist constraints. Called when the
// user touches the toolbar filter/search directly, so the toolbar stays truthful.
// Does not reload (the caller does).
func (u *ui) clearSmartCriteria() {
	u.qGenre, u.qArtist, u.qAlbum, u.smartName = "", "", "", ""
}

// applySmartPlaylist makes a smart playlist the active view: its criteria become
// the live query and the table reloads. The toolbar widgets aren't synced (they
// reflect manual state); the status bar shows the active playlist name instead.
func (u *ui) applySmartPlaylist(s SmartPlaylist) {
	u.filter = s.Filter
	u.search = s.Search
	u.qGenre = s.Genre
	u.qArtist = s.Artist
	u.qAlbum = s.Album
	u.smartName = s.Name
	u.reload()
}

// showNewSmartPlaylist opens the create dialog (a blank smart-playlist form).
func (u *ui) showNewSmartPlaylist() { u.smartPlaylistForm(nil, nil) }

// smartPlaylistForm is the shared create/edit dialog. When edit is nil it creates
// a new playlist; otherwise it pre-fills from edit and updates that one by id (so
// it can be renamed). onSaved, if set, runs after a successful save (the manage
// dialog uses it to refresh its list). The rating filter reuses the toolbar's
// options; genre/artist/album/search are optional partial (case-insensitive)
// constraints.
func (u *ui) smartPlaylistForm(edit *SmartPlaylist, onSaved func()) {
	name := widget.NewEntry()
	name.SetPlaceHolder("Playlist name")
	ratingSel := widget.NewSelect(filterLabels(), nil)
	genre := widget.NewEntry()
	genre.SetPlaceHolder("any (partial ok)")
	artist := widget.NewEntry()
	artist.SetPlaceHolder("any (partial, e.g. Wickham)")
	album := widget.NewEntry()
	album.SetPlaceHolder("any (partial ok)")
	search := widget.NewEntry()
	search.SetPlaceHolder("any text in title/artist/album/genre/filename")

	title := "New smart playlist"
	if edit != nil {
		title = "Edit smart playlist"
		name.SetText(edit.Name)
		ratingSel.SetSelected(filterLabelFor(edit.Filter))
		genre.SetText(edit.Genre)
		artist.SetText(edit.Artist)
		album.SetText(edit.Album)
		search.SetText(edit.Search)
	} else {
		ratingSel.SetSelectedIndex(0) // "All tracks"
	}

	form := widget.NewForm(
		widget.NewFormItem("Name", name),
		widget.NewFormItem("Rating", ratingSel),
		widget.NewFormItem("Genre", genre),
		widget.NewFormItem("Artist", artist),
		widget.NewFormItem("Album", album),
		widget.NewFormItem("Search", search),
	)
	hint := widget.NewLabel("Leave a field blank for \"any\". Genre/Artist/Album match " +
		"partially and ignore case (e.g. \"Wickham\" matches \"Phil Wickham\"). Criteria " +
		"combine with AND and re-evaluate every time the playlist is applied.")
	hint.Wrapping = fyne.TextWrapWord
	content := container.NewVBox(form, hint)

	d := dialog.NewCustomConfirm(title, "Save", "Cancel", content, func(ok bool) {
		if !ok {
			return
		}
		nm := strings.TrimSpace(name.Text)
		if nm == "" {
			dialog.ShowInformation(title, "Please enter a name.", u.win)
			return
		}
		sp := SmartPlaylist{
			Name:   nm,
			Filter: filterByLabel(ratingSel.Selected),
			Search: strings.TrimSpace(search.Text),
			Genre:  strings.TrimSpace(genre.Text),
			Artist: strings.TrimSpace(artist.Text),
			Album:  strings.TrimSpace(album.Text),
		}
		if edit != nil {
			sp.ID = edit.ID
			if err := u.db.UpdateSmartPlaylist(sp); err != nil {
				dialog.ShowError(fmt.Errorf("could not save (is the name already used?): %w", err), u.win)
				return
			}
		} else if err := u.db.SaveSmartPlaylist(sp); err != nil {
			dialog.ShowError(err, u.win)
			return
		}
		u.rebuildMenu() // refresh the Playlists menu

		// Show a new playlist immediately; for an edit, only re-apply if it's the
		// one currently in view (its criteria/name may have changed).
		if edit == nil || u.smartName == edit.Name {
			u.applySmartPlaylist(sp)
		}
		if onSaved != nil {
			onSaved()
		}
	}, u.win)
	d.Resize(fyne.NewSize(460, 380))
	d.Show()
}

// manageSmartPlaylists lists saved smart playlists with a Delete button each.
func (u *ui) manageSmartPlaylists() {
	lists, err := u.db.SmartPlaylists()
	if err != nil {
		dialog.ShowError(err, u.win)
		return
	}
	if len(lists) == 0 {
		dialog.ShowInformation("Smart playlists", "You haven't saved any smart playlists yet.", u.win)
		return
	}

	var dlg dialog.Dialog
	box := container.NewVBox()
	rebuild := func() {} // forward declaration for the closure below
	rebuild = func() {
		ls, err := u.db.SmartPlaylists()
		if err != nil {
			dialog.ShowError(err, u.win)
			return
		}
		box.RemoveAll()
		if len(ls) == 0 {
			box.Add(widget.NewLabel("No smart playlists."))
		}
		for _, s := range ls {
			s := s // capture
			edit := widget.NewButtonWithIcon("", theme.DocumentCreateIcon(), func() {
				u.smartPlaylistForm(&s, rebuild) // pre-filled; refresh this list on save
			})
			del := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
				if derr := u.db.DeleteSmartPlaylist(s.ID); derr != nil {
					dialog.ShowError(derr, u.win)
					return
				}
				if u.smartName == s.Name {
					u.clearSmartCriteria()
					u.reload()
				}
				u.rebuildMenu()
				rebuild()
			})
			del.Importance = widget.DangerImportance
			box.Add(container.NewBorder(nil, nil, nil, container.NewHBox(edit, del),
				widget.NewLabel(fmt.Sprintf("%s   (%s)", s.Name, smartPlaylistSummary(s)))))
		}
		box.Refresh()
	}
	rebuild()

	dlg = dialog.NewCustom("Smart playlists", "Close", container.NewVScroll(box), u.win)
	dlg.Resize(fyne.NewSize(480, 360))
	dlg.Show()
}

// smartPlaylistSummary renders a smart playlist's criteria as a short line.
func smartPlaylistSummary(s SmartPlaylist) string {
	var parts []string
	if lbl := filterLabelFor(s.Filter); lbl != "" && s.Filter != FilterAll {
		parts = append(parts, lbl)
	}
	if s.Genre != "" {
		parts = append(parts, "genre~"+s.Genre)
	}
	if s.Artist != "" {
		parts = append(parts, "artist~"+s.Artist)
	}
	if s.Album != "" {
		parts = append(parts, "album~"+s.Album)
	}
	if s.Search != "" {
		parts = append(parts, "search~"+s.Search)
	}
	if len(parts) == 0 {
		return "all tracks"
	}
	return strings.Join(parts, ", ")
}
