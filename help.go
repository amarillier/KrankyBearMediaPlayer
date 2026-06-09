package main

import (
	"net/url"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
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
	if helpWindow != nil && helpWindow.Content().Visible() {
		helpWindow.Show()
		helpWindow.RequestFocus()
		markWindowOpen(helpWindow)
		return
	}

	helpWindow = a.NewWindow(appName + " - Help")
	helpWindow.SetIcon(resourceKrankyBearMediaPlayerPng)

	helpText := `KrankyBear MediaPlayer - your music library and player

GETTING STARTED:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
• Library → Add Folder: pick a folder of music; it is scanned into the catalog.
  Add as many folders / locations as you like.
• Library → Rescan All: re-scan watched folders to pick up new or changed files.
• Your library is a small SQLite database. Title, artist, album, genre, year,
  track number and embedded album art are read from each file.

PLAYING MUSIC:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
• Double-click any track to start playing from there through the rest of the list.
• Transport (bottom bar): Previous, Play/Pause, Stop, Next.
• Playback menu: Shuffle plays the queue in a random order; Repeat → Off / All
  (loop the queue) / One (loop the current track). Both are remembered. Skipping
  with Next/Previous always moves on, even with Repeat One.
• Play Queue (Playback → Show Play Queue): see what's lined up in play order, with
  the current track marked ▶. Right-click a track → Add to Queue (or Library → Add
  Selected / Add All Shown to Queue) appends without interrupting what's playing.
  In the queue window: Play the selected track, Remove it, move it Up/Down, or
  Clear All.
• Seek bar shows elapsed / total time — drag it to jump within a track.
• Volume slider adjusts the playback level.
• The selected-row indicator follows the track that is now playing.

RATINGS & PLAY TRACKING:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
• Every track has a play count and a 1–5 star rating (empty = unplayed).
• Click the stars in a track's Rating column to set a rating; click the same star
  again to clear it. The "Rate:" buttons (bottom) and the right-click menu do the same.
• Auto rating: with no manual rating set, stars are derived from how often you've
  played a track. A manual rating always wins and is never changed automatically.
• View → Count play after: choose how much of a track must play before it counts
  as a play (25 / 50 / 75 / 90% or End of track). Skipping never counts.

FINDING & ORGANISING:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
• Search box (top): filters by title, artist, album, genre or filename as you type.
• "Show" filter (top): All, Unplayed, exact stars, or N-stars-and-up.
• Click a column header to sort; click again to reverse (the Art column doesn't sort).
• View menu: optionally show the Track # and Filename columns (off by default to
  reduce clutter; your choice is remembered).
• Copy tracks to another place: View → Selection checkboxes adds a ✓ column; tick
  rows (click the ✓ header to toggle all shown), or Library → Select All Shown.
  Then Library → Copy Selected to… lets you choose Flat or Organize into
  Artist/Album folders, then a destination - e.g. filter to 5 stars, mark all,
  copy to a USB drive. A progress dialog shows each file and lets you Cancel.
  (Name clashes get a " (2)" suffix; nothing is overwritten.)
• Right-click a track for Play, rating, Add album art (local image file or URL,
  stored in the library), Show in your file manager, and Show full path (with a
  Copy button). Rename and tag editing are coming soon.

PLAYLISTS:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
• Static playlists (Playlists menu): Save Playlist… writes the tracks you're
  currently viewing to a portable .m3u8 file, with paths relative to the file so a
  playlist and its music travel together (e.g. on a USB stick). Open Playlist…
  loads one and plays it; files still in your catalog keep their stars/play counts.
• Smart playlists (Playlists → New Smart Playlist…): save filter criteria — a
  rating, and optional Genre / Artist / Album (partial, case-insensitive — e.g.
  "Wickham" matches "Phil Wickham"), plus free-text Search — under a name. Picking
  it from the Playlists menu re-applies the criteria live (the status bar shows
  ♫ <name>). They update automatically as your library changes. Manage Smart
  Playlists… deletes ones you no longer want.

WINDOWS & SYSTEM TRAY:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
• A system-tray icon + menu mirrors the controls: Show/Hide all windows,
  Play/Pause, Previous, Next, Stop, Quit.
• Hide All Windows tucks the app away and pauses playback; Show All brings it
  back (press Play to resume).
• The main window remembers its size between launches.

PORTABLE LIBRARY (moving between machines or drives):
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
• Track locations are stored relative to each watched folder, so if a portable
  drive mounts as D:\ on one PC and E:\ on another, just use
  Library → Relocate Folder to repoint the folder — every track under it follows,
  keeping its play counts and ratings.
• The database location can be set with the -db command-line flag, the KBMP_DB
  environment variable, or View → Preferences → Database location (restart to take
  effect); by default it sits next to the app (handy on a USB stick).

PREFERENCES:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
• View → Preferences… (Cmd/Ctrl+,) gathers the common settings in one place:
  theme, "count a play after" threshold, playback volume (remembered across
  launches), the optional columns, and the database location. The same toggles
  remain on the View menu for quick one-off changes.

SMART FEATURES:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
✨ Embedded album art shown as thumbnails in the table and the now-playing area.
✨ Auto star rating from play count, with manual ratings always preserved.
✨ Theme support: Light, Dark, or System theme (View menu).

KEYBOARD SHORTCUTS:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
• Alt+P — Play / Pause
• Alt+H — Hide all windows (and pause). No shortcut to show again, by design —
  use the system tray or View → Show All Windows.
• Cmd/Ctrl+, — Preferences
• Cmd/Ctrl+Q — Quit   • Cmd/Ctrl+W — Close window   • Cmd/Ctrl+M — Minimize

KNOWN LIMITATIONS:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
⚠️  Supported audio formats: MP3, FLAC, WAV, OGG (M4A/AAC/ALAC not yet supported).
⚠️  Music-video playback is planned but not yet available.

MORE INFORMATION:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
For documentation, bug reports, or feature requests:
📦 GitHub: https://github.com/amarillier/KrankyBearMediaPlayer
📄 License: https://github.com/amarillier/KrankyBearMediaPlayer/blob/allanm/LICENSE
📝 Release Notes: Check "Help → Check for Updates"

FREE SOFTWARE - Use anywhere, anytime, any purpose!
No registration, no tracking. The only network use is a discreet update check -
once per day on launch, plus the manual "Check for Updates".
`

	helpLabel := widget.NewLabel(helpText)
	helpLabel.Wrapping = fyne.TextWrapWord

	// Links - update URLs for your project
	githubURL, _ := url.Parse("https://github.com/amarillier/KrankyBearMediaPlayer")
	githubLink := widget.NewHyperlink("Visit GitHub Repository", githubURL)
	githubLink.Alignment = fyne.TextAlignCenter

	licenseURL, _ := url.Parse("https://github.com/amarillier/KrankyBearMediaPlayer/blob/allanm/LICENSE")
	licenseLink := widget.NewHyperlink("View License", licenseURL)
	licenseLink.Alignment = fyne.TextAlignCenter

	// Create scrollable area with minimum size for better readability
	scrollContent := container.NewScroll(helpLabel)
	scrollContent.SetMinSize(fyne.NewSize(750, 550))

	// Layout with better proportions
	header := container.NewVBox(
		widget.NewLabelWithStyle(appName+" - Help", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
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
