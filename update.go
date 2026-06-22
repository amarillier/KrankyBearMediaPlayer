// Package main provides update checking dialog
// Note: About and Help dialogs have been moved to separate files:
//   - about.go: About dialog (reusable)
//   - help.go: Help dialog (reusable)
//   - dialogs.go: Update checker dialog (this file)
package main

import (
	"net/url"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"mediaplayer/internal/i18n"
)

var updateWindow fyne.Window

// checkForUpdatesManual runs the user-triggered "Check for Updates" off the UI
// goroutine (the GitHub query is a blocking network call — running it inline in the
// menu callback froze the window with a spinning cursor for its duration). The status
// bar shows progress; the result dialog is marshalled back with fyne.Do. minDays 0 =
// never throttled (manual), but it shares the cache file with the launch check.
func (u *ui) checkForUpdatesManual() {
	u.status.SetText(i18n.T("status.checking_updates"))
	go func() {
		msg, avail, remoteTag := updateChecker(updateRepoOwner, updateRepoName,
			updateRepoName, "", updateCheckStatePath(), 0)
		ahead := versionIsNewer(appVersion, remoteTag)
		fyne.Do(func() {
			u.status.SetText("")
			showUpdateDialog(u.app, msg, avail, ahead)
		})
	}()
}

// showUpdateDialog shows the update-check result. When ahead is true (the local
// build is newer than the latest published release) it shows the HardHat bear -
// "you're building ahead of release"; otherwise the normal app icon (whether the
// version is current or an update is available).
func showUpdateDialog(a fyne.App, message string, updateAvailable bool, ahead bool) {
	if updateWindow != nil && updateWindow.Content().Visible() {
		updateWindow.Show()
		updateWindow.RequestFocus()
		markWindowOpen(updateWindow)
		return
	}

	iconRes := resourceKrankyBearMediaPlayerPng
	if ahead {
		iconRes = resourceKrankyBearHardHatPng
	}

	updateWindow = a.NewWindow(appName + " - Update Check")
	updateWindow.SetIcon(iconRes)

	icon := canvas.NewImageFromResource(iconRes)
	icon.FillMode = canvas.ImageFillContain
	icon.SetMinSize(fyne.NewSize(128, 128))

	messageLabel := widget.NewLabel(message)
	messageLabel.Wrapping = fyne.TextWrapWord
	messageLabel.Alignment = fyne.TextAlignCenter

	var content *fyne.Container
	if updateAvailable {
		releaseURL, _ := url.Parse("https://github.com/amarillier/KrankyBearMediaPlayer/releases/latest")
		releaseLink := widget.NewHyperlink("Download Latest Release", releaseURL)
		releaseLink.Alignment = fyne.TextAlignCenter

		notesURL, _ := url.Parse("https://github.com/amarillier/KrankyBearMediaPlayer/blob/allanm/ReleaseNotes.txt")
		notesLink := widget.NewHyperlink("View Release Notes", notesURL)
		notesLink.Alignment = fyne.TextAlignCenter

		content = container.NewVBox(
			container.NewCenter(icon),
			widget.NewSeparator(),
			messageLabel,
			widget.NewSeparator(),
			container.NewCenter(releaseLink),
			container.NewCenter(notesLink),
		)
	} else {
		content = container.NewVBox(
			container.NewCenter(icon),
			widget.NewSeparator(),
			messageLabel,
		)
	}

	updateWindow.SetContent(container.NewPadded(content))
	updateWindow.Resize(fyne.NewSize(380, 320))

	updateWindow.SetCloseIntercept(func() {
		updateWindow.Hide()
		markWindowClosed(updateWindow)
	})

	updateWindow.Show()
	markWindowOpen(updateWindow)
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
