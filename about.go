package main

import (
	"net/url"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"mediaplayer/internal/i18n"
)

var aboutWindow fyne.Window

// showAbout displays the About dialog with app branding, version, and links
// Reusable pattern from KrankyBearClock - customize these for your app:
//   - appName: Your application name
//   - appVersion: Current version string
//   - appAuthor: Author name
//   - appCopyright: Copyright string (can use dynamic year)
//   - resourceKrankyBearMediaPlayerPng: Your embedded icon resource
//   - GitHub and License URLs
func showAbout(a fyne.App) {
	if aboutWindow != nil {
		// Re-show the existing window (reused, not rebuilt). Only pop the easter egg
		// when it was *already* open and the user opened it again.
		alreadyOpen := windowIsOpen(aboutWindow)
		aboutWindow.Show()
		aboutWindow.RequestFocus()
		markWindowOpen(aboutWindow)
		if alreadyOpen {
			showEasterEgg(a, "🐻 About to find something…")
		}
		return
	}

	aboutWindow = a.NewWindow(i18n.TC("about.win_title", map[string]string{"app_name": appName}))
	aboutWindow.SetIcon(resourceKrankyBearMediaPlayerPng)

	// App icon - adjust size as needed
	icon := canvas.NewImageFromResource(resourceKrankyBearMediaPlayerPng)
	icon.FillMode = canvas.ImageFillContain
	icon.SetMinSize(fyne.NewSize(128, 128))

	// Title and version info
	title := widget.NewLabelWithStyle(appName, fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	version := widget.NewLabel(i18n.TC("about.version", map[string]string{"v": appVersion}))
	version.Alignment = fyne.TextAlignCenter

	// Description: loaded from about_<locale>.txt (falls back to embedded English).
	description := widget.NewLabel(i18n.AboutBody(appName, appVersion, appCopyright))
	description.Alignment = fyne.TextAlignCenter
	description.Wrapping = fyne.TextWrapWord

	// Copyright and author
	copyright := widget.NewLabel(appCopyright)
	copyright.Alignment = fyne.TextAlignCenter
	author := widget.NewLabel(i18n.TC("about.by", map[string]string{"author": appAuthor}))
	author.Alignment = fyne.TextAlignCenter

	// Links - update URLs for your project
	licenseURL, _ := url.Parse("https://github.com/amarillier/KrankyBearMediaPlayer/blob/allanm/LICENSE")
	licenseLink := widget.NewHyperlink(i18n.T("about.license_link"), licenseURL)
	licenseLink.Alignment = fyne.TextAlignCenter

	githubURL, _ := url.Parse("https://github.com/amarillier/KrankyBearMediaPlayer")
	githubLink := widget.NewHyperlink(i18n.T("about.github_link"), githubURL)
	githubLink.Alignment = fyne.TextAlignCenter

	// Layout
	content := container.NewVBox(
		container.NewCenter(icon),
		widget.NewSeparator(),
		title,
		version,
		description,
		widget.NewSeparator(),
		copyright,
		author,
		widget.NewSeparator(),
		container.NewCenter(licenseLink),
		container.NewCenter(githubLink),
	)

	aboutWindow.SetContent(container.NewPadded(content))
	aboutWindow.Resize(fyne.NewSize(380, 460))

	aboutWindow.SetCloseIntercept(func() {
		aboutWindow.Hide()
		markWindowClosed(aboutWindow)
	})

	aboutWindow.Show()
	markWindowOpen(aboutWindow)
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
