// Package main - rename.go is the rename-from-tags tool: it builds a new filename
// from a track's tags using the shared pattern grammar (pattern.go) and renames the
// file on disk, then reconciles the catalog's rel_path without a re-scan. Two entry
// points: a single-file rename from the right-click row menu (live preview), and a
// batch "Rename Selected from pattern…" over the marked set (old->new preview list +
// the Copy Selected progress/cancel pattern). The extension is always preserved; a
// name clash gets a " (n)" suffix (uniqueDestPath); nothing is overwritten.
package main

import (
	"fmt"
	"log"
	"os"
	pathpkg "path"
	"path/filepath"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// renameOnDisk renames absPath to newBase within the same directory, keeping the
// original extension. It returns the new absolute path. A no-op (new name equals old)
// returns the original path unchanged; a clash with an existing file gets a " (n)"
// suffix so nothing is overwritten.
func renameOnDisk(absPath, newBase string) (string, error) {
	if newBase == "" {
		return "", fmt.Errorf("the pattern produced an empty filename")
	}
	dir := filepath.Dir(absPath)
	ext := filepath.Ext(absPath)
	desired := filepath.Join(dir, newBase+ext)
	if desired == absPath {
		return absPath, nil // unchanged
	}
	target := uniqueDestPath(dir, newBase+ext)
	if err := os.Rename(absPath, target); err != nil {
		return "", err
	}
	return target, nil
}

// newRelPath derives the post-rename rel_path: the directory of the old (slash-
// normalized) rel_path with the new file's base name. Renames stay in the same
// directory, so only the final element changes.
func newRelPath(oldRelSlash, newAbs string) string {
	base := filepath.Base(newAbs)
	dir := pathpkg.Dir(oldRelSlash)
	if dir == "." || dir == "/" {
		return base
	}
	return dir + "/" + base
}

// patternPicker returns a pattern Entry pre-filled from the given preference plus a
// preset Select that fills the entry; onChange fires for both so callers can update a
// live preview. The caller wires the entry into a form and reads its .Text on confirm.
func (u *ui) patternPicker(prefKey, fallback string, onChange func()) (*widget.Entry, *widget.Select) {
	entry := widget.NewEntry()
	entry.SetText(u.app.Preferences().StringWithFallback(prefKey, fallback))
	entry.OnChanged = func(string) { onChange() }
	presets := widget.NewSelect(renamePresets, func(s string) { entry.SetText(s) })
	presets.PlaceHolder = "Presets…"
	return entry, presets
}

// renameFromTags opens the single-file rename dialog for one row.
func (u *ui) renameFromTags(row int) {
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
	ext := filepath.Ext(path)

	preview := widget.NewLabel("")
	preview.Wrapping = fyne.TextWrapWord
	patEntry, presets := u.patternPicker(prefRenamePattern, renamePresets[1], func() {})
	update := func() {
		base := buildName(patEntry.Text, tt)
		if base == "" {
			preview.SetText("(pattern produces an empty name)")
		} else {
			preview.SetText(base + ext)
		}
	}
	patEntry.OnChanged = func(string) { update() }
	update()

	help := widget.NewLabelWithStyle(
		"Tokens: %track% %title% %artist% %album% %albumartist% %year% %genre% "+
			"(the .ext is kept).", fyne.TextAlignLeading, fyne.TextStyle{Italic: true})
	help.Wrapping = fyne.TextWrapWord

	form := widget.NewForm(
		widget.NewFormItem("Pattern", patEntry),
		widget.NewFormItem("", presets),
		widget.NewFormItem("New name", preview),
	)
	body := container.NewVBox(form, help)
	d := dialog.NewCustomConfirm("Rename file — "+filepath.Base(path), "Rename", "Cancel", body, func(ok bool) {
		if !ok {
			return
		}
		base := buildName(patEntry.Text, tt)
		if base == "" {
			dialog.ShowInformation("Rename file", "That pattern produces an empty filename.", u.win)
			return
		}
		u.app.Preferences().SetString(prefRenamePattern, patEntry.Text)
		u.applyRename(tr, base)
	}, u.win)
	d.Resize(fyne.NewSize(520, 240))
	d.Show()
}

// applyRename renames a single track's file and updates the catalog + view. If the
// track is currently playing, playback is stopped first to release the file handle
// (a rename is blocked by an open handle on Windows, same as saveTags).
func (u *ui) applyRename(tr Track, newBase string) {
	if cur, ok := u.player.Current(); ok && cur.ID == tr.ID {
		u.player.Stop()
		u.status.SetText("Stopped playback to rename file")
	}
	abs := tr.AbsPath()
	newAbs, err := renameOnDisk(abs, newBase)
	if err != nil {
		dialog.ShowError(fmt.Errorf("could not rename file: %w", err), u.win)
		return
	}
	if newAbs == abs {
		u.status.SetText("Name unchanged")
		return
	}
	rel := newRelPath(tr.RelPath, newAbs)
	if err := u.db.UpdateTrackPath(tr.ID, rel); err != nil {
		dialog.ShowError(fmt.Errorf("renamed the file but could not update the catalog: %w", err), u.win)
		return
	}
	for i := range u.tracks {
		if u.tracks[i].ID == tr.ID {
			u.tracks[i].RelPath = rel
			break
		}
	}
	u.table.Refresh()
	u.refreshNowPlaying()
	u.status.SetText("Renamed to " + filepath.Base(newAbs))
}

// renameItem is one marked track prepared for batch rename: its id, current on-disk
// path, and the tags read from it (so live preview doesn't re-read on every keystroke).
type renameItem struct {
	id   int64
	path string
	tt   trackTags
	ext  string
}

// renameSelectedFromTags opens the batch rename dialog for the marked tracks.
func (u *ui) renameSelectedFromTags() {
	if len(u.marked) == 0 {
		dialog.ShowInformation("Rename Selected from pattern",
			"No tracks are marked. Turn on View → Selection checkboxes and tick some "+
				"tracks (or Library → Select All Shown), then try again.", u.win)
		return
	}

	// Read each marked file's tags once up front; unreadable files are skipped.
	items := make([]renameItem, 0, len(u.marked))
	for id, e := range u.marked {
		tt, err := readTrackTags(e.path)
		if err != nil {
			log.Printf("batch rename: read %q: %v", e.path, err)
			continue
		}
		items = append(items, renameItem{id: id, path: e.path, tt: tt, ext: filepath.Ext(e.path)})
	}
	if len(items) == 0 {
		dialog.ShowInformation("Rename Selected from pattern",
			"Could not read tags from any of the marked files.", u.win)
		return
	}

	var patEntry *widget.Entry
	nameFor := func(it renameItem) string {
		base := buildName(patEntry.Text, it.tt)
		if base == "" {
			return "(empty — skipped)"
		}
		return base + it.ext
	}
	list := widget.NewList(
		func() int { return len(items) },
		func() fyne.CanvasObject {
			l := widget.NewLabel("")
			l.Truncation = fyne.TextTruncateEllipsis
			return l
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			o.(*widget.Label).SetText(filepath.Base(items[i].path) + "   →   " + nameFor(items[i]))
		},
	)
	var presets *widget.Select
	patEntry, presets = u.patternPicker(prefRenamePattern, renamePresets[1], func() { list.Refresh() })

	note := widget.NewLabel(fmt.Sprintf(
		"Preview for %d marked track(s). The extension is kept; name clashes get a "+
			"\" (2)\" suffix.", len(items)))
	note.Wrapping = fyne.TextWrapWord
	top := container.NewVBox(
		note,
		widget.NewForm(
			widget.NewFormItem("Pattern", patEntry),
			widget.NewFormItem("", presets),
		),
	)
	body := container.NewBorder(top, nil, nil, nil, list)
	d := dialog.NewCustomConfirm("Rename Selected from pattern", "Rename", "Cancel", body, func(ok bool) {
		if !ok {
			return
		}
		u.app.Preferences().SetString(prefRenamePattern, patEntry.Text)
		u.applyBatchRename(items, patEntry.Text)
	}, u.win)
	d.Resize(fyne.NewSize(620, 520))
	d.Show()
}

// applyBatchRename renames every item on a background goroutine with a progress /
// Cancel dialog (mirrors applyTagPatch). Items whose pattern yields an empty name are
// skipped; each rename keeps the extension and avoids overwriting.
func (u *ui) applyBatchRename(items []renameItem, pattern string) {
	// Stop playback if a target is the currently-playing track (open handle blocks
	// the rename on Windows).
	if cur, ok := u.player.Current(); ok {
		for _, it := range items {
			if it.id == cur.ID {
				u.player.Stop()
				u.status.SetText("Stopped playback to rename file")
				break
			}
		}
	}

	total := len(items)
	statusLbl := widget.NewLabel(fmt.Sprintf("Renaming %d file(s)…", total))
	statusLbl.Truncation = fyne.TextTruncateEllipsis
	prog := widget.NewProgressBar()
	prog.Max = float64(total)
	var canceled atomic.Bool
	cancelBtn := widget.NewButton("Cancel", func() { canceled.Store(true) })
	cancelBtn.Importance = widget.DangerImportance
	d := dialog.NewCustomWithoutButtons("Renaming files", container.NewVBox(
		statusLbl, prog, container.NewCenter(cancelBtn),
	), u.win)
	d.Resize(fyne.NewSize(420, 150))
	d.Show()

	go func() {
		var renamed, skipped, failed int
		for i, it := range items {
			if canceled.Load() {
				break
			}
			n, name := i+1, filepath.Base(it.path)
			fyne.Do(func() { statusLbl.SetText(fmt.Sprintf("Renaming %d of %d: %s", n, total, name)) })

			base := buildName(pattern, it.tt)
			if base == "" {
				skipped++
				fyne.Do(func() { prog.SetValue(float64(n)) })
				continue
			}
			newAbs, err := renameOnDisk(it.path, base)
			if err != nil {
				log.Printf("batch rename: %q: %v", it.path, err)
				failed++
				fyne.Do(func() { prog.SetValue(float64(n)) })
				continue
			}
			if newAbs == it.path { // unchanged
				skipped++
				fyne.Do(func() { prog.SetValue(float64(n)) })
				continue
			}
			oldRel, err := u.db.TrackRelPath(it.id)
			if err != nil {
				log.Printf("batch rename: rel_path %d: %v", it.id, err)
				failed++
				fyne.Do(func() { prog.SetValue(float64(n)) })
				continue
			}
			rel := newRelPath(oldRel, newAbs)
			if err := u.db.UpdateTrackPath(it.id, rel); err != nil {
				log.Printf("batch rename: catalog update %d: %v", it.id, err)
				failed++
				fyne.Do(func() { prog.SetValue(float64(n)) })
				continue
			}
			renamed++

			id := it.id
			fyne.Do(func() {
				for j := range u.tracks {
					if u.tracks[j].ID == id {
						u.tracks[j].RelPath = rel
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
			summary := fmt.Sprintf("Renamed %d file(s)", renamed)
			if skipped > 0 {
				summary += fmt.Sprintf("; %d skipped (empty name or unchanged)", skipped)
			}
			if failed > 0 {
				summary += fmt.Sprintf("; %d failed (see log)", failed)
			}
			title := "Files renamed"
			if wasCanceled {
				title = "Rename cancelled"
				summary = "Cancelled before finishing. " + summary
			}
			u.status.SetText(summary)
			dialog.ShowInformation(title, summary, u.win)
		})
	}()
}
