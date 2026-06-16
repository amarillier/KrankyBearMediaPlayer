// Package main - windows.go provides "hide all / show all windows" and window
// size persistence, mirroring the patterns in the TaniumQuest project so the
// two apps behave consistently.
//
// Hide all / show all: a "boss key" (and menu/tray items) hide the main window
// together with every open secondary window (Help, About, Update) and pause
// playback. Showing restores exactly the windows that were open. There is no
// hotkey to show again (you reveal via the tray/menu) and playback is NOT
// auto-resumed - both deliberate.
//
// Fyne has no Window.IsVisible(), so we can't ask a window whether it's shown.
// Instead we track "openness" ourselves: each show* function calls markWindowOpen
// and each close intercept calls markWindowClosed. hideAllWindows snapshots the
// open set (leaving the flags set) so showAllWindows can restore it.
//
// Window position is intentionally not persisted: Fyne still has no reliable
// cross-platform API to save/restore window location or display. Size only.
package main

import "fyne.io/fyne/v2"

// managedWindow couples a secondary window with our own "is it open" flag.
type managedWindow struct {
	win  fyne.Window
	open bool
}

// secondaryWindows is the registry of auxiliary windows (Help, About, Update).
var secondaryWindows []*managedWindow

// markWindowOpen records a secondary window as open (registering it on first
// sight). Call from each show* function right after the window is shown.
func markWindowOpen(w fyne.Window) {
	for _, m := range secondaryWindows {
		if m.win == w {
			m.open = true
			return
		}
	}
	secondaryWindows = append(secondaryWindows, &managedWindow{win: w, open: true})
}

// markWindowClosed records that the user closed a secondary window, so show-all
// won't bring it back. Call from each window's close intercept.
func markWindowClosed(w fyne.Window) {
	for _, m := range secondaryWindows {
		if m.win == w {
			m.open = false
			return
		}
	}
}

// windowIsOpen reports whether a secondary window is currently shown, per our own
// open/closed tracking. Use this instead of Content().Visible(), which stays true once
// the content exists even after the window is hidden/closed.
func windowIsOpen(w fyne.Window) bool {
	for _, m := range secondaryWindows {
		if m.win == w {
			return m.open
		}
	}
	return false
}

// forgetWindow drops a secondary window from the registry entirely (used by transient
// windows like easter eggs that are closed/freed rather than reused, so they don't
// accumulate stale entries).
func forgetWindow(w fyne.Window) {
	for i, m := range secondaryWindows {
		if m.win == w {
			secondaryWindows = append(secondaryWindows[:i], secondaryWindows[i+1:]...)
			return
		}
	}
}

// hideAllWindows is the "boss key": pause playback and hide the main window plus
// every open secondary window. Open flags are preserved so showAllWindows can
// restore exactly this set.
func (u *ui) hideAllWindows() {
	u.player.Pause()
	u.win.Hide()
	for _, m := range secondaryWindows {
		if m.open && m.win != nil {
			m.win.Hide()
		}
	}
}

// showAllWindows restores the main window and every window that was open when
// hidden. Playback is not auto-resumed (the user presses play when ready).
func (u *ui) showAllWindows() {
	u.win.Show()
	u.win.RequestFocus()
	for _, m := range secondaryWindows {
		if m.open && m.win != nil {
			m.win.Show()
		}
	}
}

// ---- Window size persistence (size only; no position - Fyne limitation) ----

const (
	prefWindowWidth  = "windowWidth"
	prefWindowHeight = "windowHeight"

	mainWindowDefaultWidth  = float32(980)
	mainWindowDefaultHeight = float32(640)

	// Sanity bounds: below the floor is unusably small; above the ceiling
	// suggests corrupted prefs or a multi-monitor edge case we won't honor.
	mainWindowMinWidth  = float32(400)
	mainWindowMinHeight = float32(300)
	mainWindowMaxWidth  = float32(8000)
	mainWindowMaxHeight = float32(8000)
)

// mainWindowLaunchSize returns the saved window size if present and sane,
// otherwise the default.
func mainWindowLaunchSize(a fyne.App) fyne.Size {
	sw := float32(a.Preferences().FloatWithFallback(prefWindowWidth, float64(mainWindowDefaultWidth)))
	sh := float32(a.Preferences().FloatWithFallback(prefWindowHeight, float64(mainWindowDefaultHeight)))
	if sw < mainWindowMinWidth || sh < mainWindowMinHeight ||
		sw > mainWindowMaxWidth || sh > mainWindowMaxHeight {
		return fyne.NewSize(mainWindowDefaultWidth, mainWindowDefaultHeight)
	}
	return fyne.NewSize(sw, sh)
}

// saveMainWindowGeometry persists the current window size. Skips when the size
// is below the floor (window minimized/hidden) so we don't save a useless size.
func saveMainWindowGeometry(a fyne.App, w fyne.Window) {
	if w == nil {
		return
	}
	sz := w.Canvas().Size()
	if sz.Width < mainWindowMinWidth || sz.Height < mainWindowMinHeight {
		return
	}
	a.Preferences().SetFloat(prefWindowWidth, float64(sz.Width))
	a.Preferences().SetFloat(prefWindowHeight, float64(sz.Height))
}
