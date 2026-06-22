// Package main - queue.go is the Play Queue window: a live list of the tracks
// queued for playback (in play order, honouring shuffle), with controls to jump
// to a track, remove one, reorder, or clear the queue. Tracks reach the queue via
// "Add to Queue" (right-click / Playback menu) or by playing from the library.
package main

import (
	"fmt"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"mediaplayer/internal/i18n"
)

// queueWindow is the single Play Queue window (reused across opens, like Help).
var queueWindow fyne.Window

// queueRowText renders a queued track as "Artist — Title", falling back to the
// filename when the title tag is empty.
func queueRowText(t Track) string {
	title := t.Title
	if title == "" {
		title = filepath.Base(t.RelPath)
	}
	if t.Artist == "" {
		return title
	}
	return t.Artist + " — " + title
}

// showQueue opens (or re-focuses) the Play Queue window.
func (u *ui) showQueue() {
	if queueWindow != nil && queueWindow.Content().Visible() {
		queueWindow.Show()
		queueWindow.RequestFocus()
		markWindowOpen(queueWindow)
		u.refreshQueue()
		return
	}

	queueWindow = u.app.NewWindow(appName + " - " + i18n.T("queue.win_title"))
	queueWindow.SetIcon(resourceKrankyBearMediaPlayerPng)

	u.queueList = widget.NewList(
		func() int { return len(u.queueTracks) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			lbl := o.(*widget.Label)
			if i < 0 || i >= len(u.queueTracks) {
				return
			}
			prefix := "    "
			if i == u.queueCurrent {
				prefix = "▶  "
			}
			lbl.TextStyle.Bold = i == u.queueCurrent
			lbl.SetText(fmt.Sprintf("%s%d. %s", prefix, i+1, queueRowText(u.queueTracks[i])))
		},
	)
	u.queueList.OnSelected = func(id widget.ListItemID) { u.queueSel = id }
	u.queueList.OnUnselected = func(widget.ListItemID) { u.queueSel = -1 }

	playSelBtn := widget.NewButtonWithIcon(i18n.T("queue.play"), theme.MediaPlayIcon(), func() {
		if u.queueSel >= 0 {
			u.player.JumpTo(u.queueSel)
		}
	})
	removeBtn := widget.NewButtonWithIcon(i18n.T("queue.remove"), theme.DeleteIcon(), func() {
		if u.queueSel >= 0 {
			sel := u.queueSel
			u.player.RemoveAt(sel) // triggers refreshQueue via OnChange
			u.selectQueueRow(sel)  // keep the cursor near where the row was
		}
	})
	upBtn := widget.NewButtonWithIcon(i18n.T("queue.up"), theme.MoveUpIcon(), func() {
		if u.queueSel > 0 {
			to := u.queueSel - 1
			u.player.MoveAt(u.queueSel, to)
			u.selectQueueRow(to)
		}
	})
	downBtn := widget.NewButtonWithIcon(i18n.T("queue.down"), theme.MoveDownIcon(), func() {
		if u.queueSel >= 0 && u.queueSel < len(u.queueTracks)-1 {
			to := u.queueSel + 1
			u.player.MoveAt(u.queueSel, to)
			u.selectQueueRow(to)
		}
	})
	clearBtn := widget.NewButtonWithIcon(i18n.T("queue.clear_all"), theme.ContentClearIcon(), func() {
		if len(u.queueTracks) == 0 {
			return
		}
		dialog.ShowConfirm(i18n.T("queue.clear_title"),
			i18n.T("queue.clear_confirm"),
			func(ok bool) {
				if ok {
					u.player.ClearQueue()
				}
			}, queueWindow)
	})
	clearBtn.Importance = widget.DangerImportance

	controls := container.NewHBox(playSelBtn, removeBtn, upBtn, downBtn, clearBtn)
	content := container.NewBorder(
		widget.NewLabelWithStyle(i18n.T("queue.win_title"), fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		controls, nil, nil, u.queueList,
	)
	queueWindow.SetContent(content)
	queueWindow.Resize(fyne.NewSize(460, 520))

	queueWindow.SetCloseIntercept(func() {
		queueWindow.Hide()
		markWindowClosed(queueWindow)
	})

	u.refreshQueue()
	queueWindow.Show()
	markWindowOpen(queueWindow)
}

// selectQueueRow restores the list selection to row i (clamped) after the queue
// changes, or clears it when the queue is empty. Runs on the UI thread.
func (u *ui) selectQueueRow(i int) {
	if u.queueList == nil {
		return
	}
	if n := len(u.queueTracks); n == 0 {
		u.queueList.UnselectAll()
		u.queueSel = -1
		return
	} else if i >= n {
		i = n - 1
	}
	if i < 0 {
		u.queueList.UnselectAll()
		u.queueSel = -1
		return
	}
	u.queueSel = i
	u.queueList.Select(i)
}

// refreshQueue pulls a fresh snapshot of the player's queue and updates the list.
// Cheap no-op when the Play Queue window has never been opened. Runs on the UI
// thread (called from refreshNowPlaying, itself wrapped in fyne.Do).
func (u *ui) refreshQueue() {
	if u.queueList == nil {
		return
	}
	u.queueTracks, u.queueCurrent = u.player.QueueView()
	if u.queueSel >= len(u.queueTracks) {
		u.queueSel = -1
	}
	u.queueList.Refresh()
}
