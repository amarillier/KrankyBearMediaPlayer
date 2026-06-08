package main

import (
	"net/url"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
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
	if aboutWindow != nil && aboutWindow.Content().Visible() {
		aboutWindow.Show()
		aboutWindow.RequestFocus()
		markWindowOpen(aboutWindow)
		return
	}

	aboutWindow = a.NewWindow(appName + " - About")
	aboutWindow.SetIcon(resourceKrankyBearMediaPlayerPng)

	// App icon - adjust size as needed
	icon := canvas.NewImageFromResource(resourceKrankyBearMediaPlayerPng)
	icon.FillMode = canvas.ImageFillContain
	icon.SetMinSize(fyne.NewSize(128, 128))

	// Title and version info
	title := widget.NewLabelWithStyle(appName, fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	version := widget.NewLabel("Version: " + appVersion)
	version.Alignment = fyne.TextAlignCenter

	// Description - customize for your app
	description := widget.NewLabel("A comprehensive media player")
	description.Alignment = fyne.TextAlignCenter
	description.Wrapping = fyne.TextWrapWord

	// Copyright and author
	copyright := widget.NewLabel(appCopyright)
	copyright.Alignment = fyne.TextAlignCenter
	author := widget.NewLabel("By " + appAuthor)
	author.Alignment = fyne.TextAlignCenter

	// Links - update URLs for your project
	licenseURL, _ := url.Parse("https://github.com/amarillier/KrankyBearMediaPlayer/blob/allanm/LICENSE")
	licenseLink := widget.NewHyperlink("License Information", licenseURL)
	licenseLink.Alignment = fyne.TextAlignCenter

	githubURL, _ := url.Parse("https://github.com/amarillier/KrankyBearMediaPlayer")
	githubLink := widget.NewHyperlink("GitHub Repository", githubURL)
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
