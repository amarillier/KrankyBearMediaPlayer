// Package main - folders.go is the watched-folders manager: a dialog to view,
// add, rescan, and remove the library's watched folders. Adding/rescanning
// reuses scanFolders; removing drops the folder and (via ON DELETE CASCADE) its
// catalogued tracks, but never touches files on disk.
package main

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// manageFolders opens the watched-folders manager.
func (u *ui) manageFolders() {
	box := container.NewVBox()

	var rebuild func()
	rebuild = func() {
		folders, err := u.db.Folders()
		if err != nil {
			dialog.ShowError(err, u.win)
			return
		}
		box.RemoveAll()
		if len(folders) == 0 {
			box.Add(widget.NewLabel("No watched folders yet — click \"Add Folder…\" below."))
		}
		for _, f := range folders {
			f := f // capture
			n, _ := u.db.CountTracksInFolder(f.ID)
			label := widget.NewLabel(fmt.Sprintf("%s\n%d track(s)", f.Path, n))
			label.Wrapping = fyne.TextWrapWord

			relocate := widget.NewButtonWithIcon("Relocate…", theme.FolderOpenIcon(), func() {
				u.relocateFolderTo(f, rebuild)
			})
			rescan := widget.NewButtonWithIcon("Rescan", theme.ViewRefreshIcon(), func() {
				u.scanFolders([]Folder{f}, rebuild) // refresh the count when done
			})
			del := widget.NewButtonWithIcon("Remove", theme.DeleteIcon(), func() {
				u.confirmRemoveFolder(f, rebuild)
			})
			del.Importance = widget.DangerImportance
			box.Add(container.NewBorder(nil, nil, nil, container.NewHBox(relocate, rescan, del), label))
			box.Add(widget.NewSeparator())
		}
		box.Refresh()
	}
	rebuild()

	addBtn := widget.NewButtonWithIcon("Add Folder…", theme.FolderOpenIcon(), func() {
		dialog.ShowFolderOpen(func(list fyne.ListableURI, err error) {
			if err != nil || list == nil {
				return
			}
			path := list.Path()
			id, err := u.db.AddFolder(path, true)
			if err != nil {
				dialog.ShowError(err, u.win)
				return
			}
			u.scanFolders([]Folder{{ID: id, Path: path, Recursive: true}}, rebuild)
			rebuild() // show the folder immediately; the count fills in after the scan
		}, u.win)
	})
	note := widget.NewLabel("Relocate repoints a folder at a new location (e.g. a moved or " +
		"re-lettered drive) and instantly moves its tracks — no re-scan needed. Rescan picks up " +
		"new/changed files. Remove takes a folder's tracks out of the library but never deletes " +
		"files on disk.")
	note.Wrapping = fyne.TextWrapWord

	content := container.NewBorder(nil, container.NewVBox(widget.NewSeparator(), addBtn, note),
		nil, nil, container.NewVScroll(box))
	d := dialog.NewCustom("Watched folders", "Close", content, u.win)
	d.Resize(fyne.NewSize(640, 460))
	d.Show()
}

// relocateFolderTo repoints folder f's root at a new path picked by the user,
// then reloads and runs onDone (to refresh the manager). Tracks are stored
// relative to the root, so this instantly relocates them all with no re-scan -
// the fix for a drive that mounts at a different path/letter.
func (u *ui) relocateFolderTo(f Folder, onDone func()) {
	dialog.ShowFolderOpen(func(list fyne.ListableURI, err error) {
		if err != nil || list == nil {
			return
		}
		if err := u.db.RelocateFolder(f.ID, list.Path()); err != nil {
			dialog.ShowError(err, u.win)
			return
		}
		u.reload()
		u.status.SetText("Relocated to: " + list.Path())
		if onDone != nil {
			onDone()
		}
	}, u.win)
}

// confirmRemoveFolder asks before removing a watched folder (and its catalogued
// tracks), then reloads the library and runs onRemoved (to refresh the manager).
func (u *ui) confirmRemoveFolder(f Folder, onRemoved func()) {
	n, _ := u.db.CountTracksInFolder(f.ID)
	msg := fmt.Sprintf("Remove this watched folder and its %d catalogued track(s) from the "+
		"library?\n\n%s\n\nThe files on disk are NOT deleted.", n, f.Path)
	dialog.ShowConfirm("Remove folder", msg, func(ok bool) {
		if !ok {
			return
		}
		if err := u.db.DeleteFolder(f.ID); err != nil {
			dialog.ShowError(err, u.win)
			return
		}
		u.reload()
		u.status.SetText("Removed folder: " + f.Path)
		onRemoved()
	}, u.win)
}
