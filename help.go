package main

import (
	"net/url"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"mediaplayer/internal/i18n"
)

var helpWindow fyne.Window

// showHelp displays comprehensive help documentation
// Reusable pattern from KrankyBearClock - customize these for your app:
//   - appName: Your application name
//   - resourceKrankyBearMediaPlayerPng: Your embedded icon resource
//   - helpText: Your application's help content (see below for structure)
//   - GitHub and License URLs
//
// Help text structure recommendation:
//   - Use section headers with visual separators (━━━)
//   - Group related features together
//   - Include tips, tricks, and known limitations
//   - Add keyboard shortcuts
//   - Provide links to external resources
func showHelp(a fyne.App) {
	if helpWindow != nil {
		// Re-show the existing window (reused, not rebuilt). Only pop the easter egg
		// when it was *already* open and the user opened it again.
		alreadyOpen := windowIsOpen(helpWindow)
		helpWindow.Show()
		helpWindow.RequestFocus()
		markWindowOpen(helpWindow)
		if alreadyOpen {
			showEasterEgg(a, "🐻 Help is on the way!")
		}
		return
	}

	helpWindow = a.NewWindow(i18n.TC("windows.help_title", map[string]string{"app_name": appName}))
	helpWindow.SetIcon(resourceKrankyBearMediaPlayerPng)

	helpText := i18n.HelpBody(appName)

	helpLabel := widget.NewLabel(helpText)
	helpLabel.Wrapping = fyne.TextWrapWord

	// Links - update URLs for your project
	githubURL, _ := url.Parse("https://github.com/amarillier/KrankyBearMediaPlayer")
	githubLink := widget.NewHyperlink(i18n.T("help.github_link"), githubURL)
	githubLink.Alignment = fyne.TextAlignCenter

	licenseURL, _ := url.Parse("https://github.com/amarillier/KrankyBearMediaPlayer/blob/allanm/LICENSE")
	licenseLink := widget.NewHyperlink(i18n.T("help.license_link"), licenseURL)
	licenseLink.Alignment = fyne.TextAlignCenter

	// Create scrollable area with minimum size for better readability
	scrollContent := container.NewScroll(helpLabel)
	scrollContent.SetMinSize(fyne.NewSize(750, 550))

	// Layout with better proportions
	header := container.NewVBox(
		widget.NewLabelWithStyle(i18n.TC("windows.help_title", map[string]string{"app_name": appName}), fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewSeparator(),
	)

	footer := container.NewVBox(
		widget.NewSeparator(),
		container.NewCenter(container.NewHBox(githubLink, licenseLink)),
	)

	content := container.NewBorder(header, footer, nil, nil, scrollContent)

	helpWindow.SetContent(container.NewPadded(content))
	helpWindow.Resize(fyne.NewSize(850, 700))

	helpWindow.SetCloseIntercept(func() {
		helpWindow.Hide()
		markWindowClosed(helpWindow)
	})

	helpWindow.Show()
	markWindowOpen(helpWindow)
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
