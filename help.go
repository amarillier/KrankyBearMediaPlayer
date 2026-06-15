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
• Library → Manage Folders: see every watched folder with its track count; add,
  relocate (repoint at a moved drive), rescan, or remove one. Removing takes its
  tracks out of the library but never deletes files on disk.
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
• Playback → ReplayGain: when on, tracks tagged with ReplayGain play at a
  consistent loudness (peak-limited so boosted tracks don't clip). Off by default.
• Playback → Track transition: Gap (default), Gapless (tracks butt up with no
  silence), or Crossfade (the outgoing track fades out as the next fades in).
  Gapless/Crossfade are experimental.
• Play Queue (Playback → Show Play Queue): see what's lined up in play order, with
  the current track marked ▶. Right-click a track → Add to Queue (or Library → Add
  Selected / Add All Shown to Queue) appends without interrupting what's playing.
  In the queue window: Play the selected track, Remove it, move it Up/Down, or
  Clear All.
• Hover a toolbar or transport button to see a tooltip explaining what it does.
• Seek bar shows elapsed / total time — drag it to jump within a track.
• Two volume controls sit at the bottom right:
  - "App" adjusts this player's own level only (remembered across launches).
  - "Sys" drives your computer's master output volume, with a mute toggle beside
    it. It stays in sync with the system level — including changes you make with
    the volume keys or the OS mixer — and moves the whole machine's volume, not
    just this app.
• The selected-row indicator follows the track that is now playing.

RATINGS & PLAY TRACKING:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
• Ratings are manual: set a 1–5 star rating yourself. Play count is tracked
  separately (its own column) and is NOT turned into stars.
• Click the stars in a track's Rating column to set a rating; click the same star
  again to clear it (back to no stars). The "Rate:" buttons (bottom) and the
  right-click menu do the same. All five stars are clickable.
• View → Count play after: choose how much of a track must play before its play
  count goes up (25 / 50 / 75 / 90% or End of track). Skipping never counts.
• Tip: filter by Plays (column filter) to find, say, everything you've played 5
  times, then rate those tracks however you like.

FINDING & ORGANISING:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
• Search box (top): filters by title, artist, album, genre or filename as you type.
• Column filters (View → Show Column Filters): a row of per-column boxes — type in
  Title, Artist, Album, Genre to match a substring; Year and Plays accept a pattern
  (e.g. Year 202[456] matches 2024-2026; Plays 5 = played exactly five times, 0 =
  never played). They combine with each
  other and the search/Show filter. Clear them with the ✕ button; hiding the row
  also clears them.
• "Show" filter (top): All, Unrated, exact stars, or N-stars-and-up.
• Click a column header to sort; click again to reverse (the Art column doesn't sort).
• View menu: optionally show the Length, Format, Bitrate, Genre, Track # and
  Filename columns (Length is on by default, the rest off; choices are remembered).
  Length and Bitrate fill in shortly after launch/scan as the app reads each
  track's playback time in the background, so they may be blank for a moment.
• Jump to the playing track: click its name in the bottom bar, or use the toolbar
  "Now Playing" button (next to Rescan). Handy after scrolling, searching, or
  sorting away. (It only locates the track — it doesn't start or stop playback.)
• Picks up where you left off: the app remembers your sort order and, on relaunch,
  selects and scrolls to the track you were last playing (it does not auto-play).
• Copy tracks to another place: View → Selection checkboxes adds a ✓ column; tick
  rows (click the ✓ header to toggle all shown), or Library → Select All Shown.
  Then Library → Copy Selected to… lets you choose Flat or Organize into
  Artist/Album folders, then a destination - e.g. filter to 5 stars, mark all,
  copy to a USB drive. A progress dialog shows each file and lets you Cancel.
  (Name clashes get a " (2)" suffix; nothing is overwritten.)
• Right-click a track for Play, rating, Add album art (local image file or URL,
  stored in the library), Show in your file manager, and Show full path (with a
  Copy button).
• Right-click Rename file… / Edit tags… normally act on just that track. But if
  you right-click a track that's marked (✓), they act on the whole marked set
  instead — the menu label says which (e.g. "Edit tags of 5 marked…").
• Rename file… (right-click) renames the file on disk from its tags using a
  pattern of tokens — %track% %title% %artist% %album% %albumartist% %year%
  %genre% — e.g. "%track% %title% - %artist%". A live preview shows the new name
  before you apply; the extension is always kept and the catalog updates in place
  (no re-scan). %track% is zero-padded to two digits.
• Edit tags… (right-click) opens an editor for title, artist, album, album
  artist, year, track #, genre, comment, and the embedded cover image. Changes
  are written back into the file for MP3 and FLAC; OGG and WAV open read-only for
  now. Tags from filename… (in that editor) parses the file's name into the fields
  using the same token pattern, for you to review before saving — it never writes
  on its own. If you edit the track that's currently playing, playback stops first
  so the file can be saved. The catalog (and any thumbnail) updates straight away.
• Edit tags of several at once: mark tracks (Selection checkboxes / Select All
  Shown), then Library → Edit Tags of Selected… You can combine three kinds of
  change in one pass: (1) From filename — set title/artist/track #/etc. on each
  track by parsing its name with a token pattern (the bulk form of Tags from
  filename; files that don't match are left alone); (2) Set fields — tick Artist,
  Album, Album Artist, Genre, Year or Comment to give them one value across the
  whole selection (these override the pattern); (3) Cover art — set one image as
  the cover for all, or remove the cover from all. A progress dialog shows each
  file with a Cancel button; OGG/WAV are skipped.
• Rename several at once: mark tracks, then Library → Rename Selected from
  pattern… A preview list shows each old → new name; apply renames them all (name
  clashes get a " (2)" suffix, nothing is overwritten) with a Cancel button.

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
  Playlists… lets you edit a saved playlist's criteria (the pencil button, which
  can also rename it) or delete ones you no longer want.

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
• View → Preferences… (Alt+R) gathers the common settings in one place:
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
• Alt+→ — Next track   • Alt+← — Previous track
• Alt+H — Hide all windows (and pause). No shortcut to show again, by design —
  use the system tray or View → Show All Windows.
• Alt+R — Preferences
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
