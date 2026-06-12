package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/driver/desktop"
)

const (
	// appName    = "KrankyBear MediaPlayer"
	appVersion = "0.5.1" // see FyneApp.toml
	appAuthor  = "Allan Marillier"
)

var appName = "KrankyBear MediaPlayer"
var appCopyright = buildCopyrightNotice()

func buildCopyrightNotice() string {
	const startYear = 2026
	currentYear := time.Now().Year()
	if currentYear <= startYear {
		return "Copyright (c) Allan Marillier, 2026"
	}
	return fmt.Sprintf("Copyright (c) Allan Marillier, 2026-%d", currentYear)
}

func main() {
	dbFlag := flag.String("db", "", "path to the media library database file (overrides the default/saved location)")
	flag.Parse()

	a := app.NewWithID("com.github.amarillier.KrankyBearMediaPlayer")
	a.SetIcon(resourceKrankyBearMediaPlayerPng)
	loadTheme(a)

	dbPath := *dbFlag
	if dbPath == "" {
		dbPath = resolveDBPath(a)
	}
	db, err := openDB(dbPath)
	if err != nil {
		log.Fatalf("cannot open library database %q: %v", dbPath, err)
	}
	defer db.Close()
	log.Printf("%s — library database: %s", appName, dbPath)

	player := NewPlayer(db)

	win := a.NewWindow(appName)
	win.SetIcon(resourceKrankyBearMediaPlayerPng)
	win.Resize(mainWindowLaunchSize(a)) // restore previous size (size only - Fyne can't do position)

	u := buildMainWindow(a, win, db, player)
	win.SetMainMenu(buildMenu(a, u))
	setupSystemTray(a, u)
	registerHotkeys(win, u)
	checkForUpdatesAuto(a) // discreet once-per-day check; dialog only if an update exists

	// Closing the window quits the app (previously it only hid, so the process
	// lingered). Defer via fyne.Do so the quit runs on a clean loop iteration,
	// outside whatever callback (close-intercept / menu popup) triggered it -
	// the main-menu Quit hangs on Windows otherwise.
	win.SetCloseIntercept(func() { fyne.Do(u.quit) })
	win.ShowAndRun()
}

// registerHotkeys wires the desktop accelerators on the main window's canvas:
//   - Alt+P: fast play/pause toggle
//   - Alt+H: "boss key" - hide all windows AND pause (no hotkey to show again;
//     reveal via the tray/menu, then press play to resume)
//   - Alt+Right / Alt+Left: next / previous track
//   - Alt+R: Preferences
//
// Modifier/key choices are constrained by what actually fires on macOS here:
// Cmd/Ctrl-modified canvas shortcuts don't trigger at all, and Alt+<punctuation>
// (e.g. Alt+,) doesn't either - Option+comma yields a special character, not a
// clean KeyComma event. Alt+<letter> works, so every app shortcut uses that.
// These MUST be on the canvas, not only as menu-item .Shortcut accelerators,
// which don't fire reliably. Cmd+H is avoided on macOS (= Hide application).
// Canvas shortcuts fire on the focused window, so Alt+H can't un-hide.
func registerHotkeys(win fyne.Window, u *ui) {
	cnv := win.Canvas()
	add := func(key fyne.KeyName, mod fyne.KeyModifier, fn func()) {
		cnv.AddShortcut(&desktop.CustomShortcut{KeyName: key, Modifier: mod},
			func(fyne.Shortcut) { fyne.Do(fn) })
	}
	add(fyne.KeyP, fyne.KeyModifierAlt, u.onPlayPause)
	add(fyne.KeyH, fyne.KeyModifierAlt, u.hideAllWindows)
	add(fyne.KeyRight, fyne.KeyModifierAlt, u.player.Next)
	add(fyne.KeyLeft, fyne.KeyModifierAlt, u.player.Prev)
	add(fyne.KeyR, fyne.KeyModifierAlt, u.showPreferences)
}

// setupSystemTray adds a tray icon + menu mirroring the main controls, on the
// desktop platforms that support it. Tray callbacks fire off the UI goroutine,
// so any widget access is marshalled with fyne.Do.
func setupSystemTray(a fyne.App, u *ui) {
	desk, ok := a.(desktop.App)
	if !ok {
		return // not a desktop driver (e.g. mobile/web)
	}
	menu := fyne.NewMenu(appName,

		fyne.NewMenuItem("Show All Windows", func() { fyne.Do(u.showAllWindows) }),
		fyne.NewMenuItem("Hide All Windows", func() { fyne.Do(u.hideAllWindows) }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Play / Pause", func() { fyne.Do(u.onPlayPause) }),
		fyne.NewMenuItem("Previous", u.player.Prev),
		fyne.NewMenuItem("Next", u.player.Next),
		fyne.NewMenuItem("Stop", u.player.Stop),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Preferences…", func() { fyne.Do(u.showPreferences) }),

		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Quit", func() { fyne.Do(u.quit) }), // tray runs off the main goroutine
	)
	desk.SetSystemTrayMenu(menu)
	desk.SetSystemTrayIcon(resourceKrankyBearMediaPlayerPng)
}

// defaultDBName is the catalog database's filename in default/portable locations.
const defaultDBName = "KrankyBearMediaPlayer.db"

// resolveDBPath picks where the catalog lives. Order of preference:
//  1. KBMP_DB environment variable
//  2. a saved dbPath preference (set when the user picks a custom location)
//  3. beside the executable - ideal for a portable app+DB on a USB drive
//  4. the app's per-user storage dir (always writable) as a fallback
func resolveDBPath(a fyne.App) string {
	if v := os.Getenv("KBMP_DB"); v != "" {
		return v
	}
	if p := a.Preferences().String(prefDBPath); p != "" {
		return p
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		if dirWritable(dir) {
			return filepath.Join(dir, defaultDBName)
		}
	}
	if root := a.Storage().RootURI(); root != nil {
		return filepath.Join(root.Path(), defaultDBName)
	}
	return defaultDBName // last resort: current working directory
}

// dirWritable reports whether we can create files in dir (probe + remove).
func dirWritable(dir string) bool {
	probe := filepath.Join(dir, ".kbmp_write_test")
	f, err := os.OpenFile(probe, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(probe)
	return true
}

// buildMenu builds the application main menu. Help/About/Update/Theme reuse the
// existing template dialogs; File/Library drive the catalog.
func buildMenu(a fyne.App, u *ui) *fyne.MainMenu {
	fileMenu := fyne.NewMenu("Library",
		fyne.NewMenuItem("Add Folder…", u.addFolder),
		fyne.NewMenuItem("Manage Folders…", u.manageFolders),
		fyne.NewMenuItem("Rescan All", u.rescanAll),
		fyne.NewMenuItem("Relocate Folder… (moved drive)", u.relocateFolder),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Select All Shown", u.selectAllShown),
		fyne.NewMenuItem("Clear Selection", u.clearSelection),
		fyne.NewMenuItem("Copy Selected to…", u.copySelectedTo),
		fyne.NewMenuItem("Edit Tags of Selected…", u.editTagsOfSelected),
		fyne.NewMenuItem("Add Selected to Queue", u.enqueueSelected),
		fyne.NewMenuItem("Add All Shown to Queue", u.enqueueShown),
		fyne.NewMenuItemSeparator(),
		// Defer via fyne.Do: quitting directly from the menu popup's click handler
		// hangs on Windows (closes the window from inside the popup callback).
		fyne.NewMenuItem("Quit", func() { fyne.Do(u.quit) }),
	)

	hideItem := fyne.NewMenuItem("Hide All Windows", u.hideAllWindows)
	hideItem.Shortcut = &desktop.CustomShortcut{KeyName: fyne.KeyH, Modifier: fyne.KeyModifierAlt}
	playPauseItem := fyne.NewMenuItem("Play / Pause", u.onPlayPause)
	playPauseItem.Shortcut = &desktop.CustomShortcut{KeyName: fyne.KeyP, Modifier: fyne.KeyModifierAlt}
	prevItem := fyne.NewMenuItem("Previous", u.player.Prev)
	prevItem.Shortcut = &desktop.CustomShortcut{KeyName: fyne.KeyLeft, Modifier: fyne.KeyModifierAlt}
	nextItem := fyne.NewMenuItem("Next", u.player.Next)
	nextItem.Shortcut = &desktop.CustomShortcut{KeyName: fyne.KeyRight, Modifier: fyne.KeyModifierAlt}

	// Playback menu: transport plus shuffle/repeat (state persists via prefs).
	shuffleItem := fyne.NewMenuItem("Shuffle", u.toggleShuffle)
	shuffleItem.Checked = u.player.Shuffle()
	curRepeat := u.player.Repeat()
	repeatItemFor := func(label string, m RepeatMode) *fyne.MenuItem {
		it := fyne.NewMenuItem(label, func() { u.setRepeat(m) })
		it.Checked = curRepeat == m
		return it
	}
	repeatItem := fyne.NewMenuItem("Repeat", nil)
	repeatItem.ChildMenu = fyne.NewMenu("",
		repeatItemFor("Off", RepeatOff),
		repeatItemFor("Repeat All", RepeatAll),
		repeatItemFor("Repeat One", RepeatOne),
	)
	rgItem := fyne.NewMenuItem("ReplayGain (volume normalization)", u.toggleReplayGain)
	rgItem.Checked = a.Preferences().BoolWithFallback(prefReplayGain, false)

	curTrans := transitionMode(a.Preferences().IntWithFallback(prefTransition, int(transGap)))
	transItemFor := func(label string, m transitionMode) *fyne.MenuItem {
		it := fyne.NewMenuItem(label, func() { u.setTransition(m) })
		it.Checked = curTrans == m
		return it
	}
	transitionItem := fyne.NewMenuItem("Track transition", nil)
	transitionItem.ChildMenu = fyne.NewMenu("",
		transItemFor("Gap (default)", transGap),
		transItemFor("Gapless (experimental)", transGapless),
		transItemFor("Crossfade (experimental)", transCrossfade),
	)

	playbackMenu := fyne.NewMenu("Playback",
		playPauseItem,
		prevItem,
		nextItem,
		fyne.NewMenuItem("Stop", u.player.Stop),
		fyne.NewMenuItemSeparator(),
		shuffleItem,
		repeatItem,
		rgItem,
		transitionItem,
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Show Play Queue", u.showQueue),
	)

	// Playlists menu: static .m3u8 files plus dynamic ("smart") playlists. Saved
	// smart playlists are listed inline so a click applies them.
	playlistItems := []*fyne.MenuItem{
		fyne.NewMenuItem("Open Playlist…", u.openPlaylist),
		fyne.NewMenuItem("Save Playlist…", u.savePlaylist),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("New Smart Playlist…", u.showNewSmartPlaylist),
	}
	if smarts, err := u.db.SmartPlaylists(); err == nil && len(smarts) > 0 {
		playlistItems = append(playlistItems, fyne.NewMenuItemSeparator())
		for _, s := range smarts {
			s := s // capture per item
			it := fyne.NewMenuItem(s.Name, func() { u.applySmartPlaylist(s) })
			it.Checked = u.smartName == s.Name
			playlistItems = append(playlistItems, it)
		}
		playlistItems = append(playlistItems, fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("Manage Smart Playlists…", u.manageSmartPlaylists))
	}
	playlistsMenu := fyne.NewMenu("Playlists", playlistItems...)

	// Checkable toggles for the optional columns (state persists via prefs).
	prefs := a.Preferences()
	trackColItem := fyne.NewMenuItem("Show Track # Column", func() { u.toggleColumn(prefShowTrackCol) })
	trackColItem.Checked = prefs.BoolWithFallback(prefShowTrackCol, false)
	fileColItem := fyne.NewMenuItem("Show Filename Column", func() { u.toggleColumn(prefShowFilenameCol) })
	fileColItem.Checked = prefs.BoolWithFallback(prefShowFilenameCol, false)
	selColItem := fyne.NewMenuItem("Selection Checkboxes", func() { u.toggleColumn(prefShowSelectCol) })
	selColItem.Checked = prefs.BoolWithFallback(prefShowSelectCol, false)
	durColItem := fyne.NewMenuItem("Show Length Column", func() { u.toggleColumn(prefShowDurationCol) })
	durColItem.Checked = prefs.BoolWithFallback(prefShowDurationCol, true)
	fmtColItem := fyne.NewMenuItem("Show Format Column", func() { u.toggleColumn(prefShowFormatCol) })
	fmtColItem.Checked = prefs.BoolWithFallback(prefShowFormatCol, false)
	brColItem := fyne.NewMenuItem("Show Bitrate Column", func() { u.toggleColumn(prefShowBitrateCol) })
	brColItem.Checked = prefs.BoolWithFallback(prefShowBitrateCol, false)
	genreColItem := fyne.NewMenuItem("Show Genre Column", func() { u.toggleColumn(prefShowGenreCol) })
	genreColItem.Checked = prefs.BoolWithFallback(prefShowGenreCol, false)
	colFilterItem := fyne.NewMenuItem("Show Column Filters", u.toggleColumnFilters)
	colFilterItem.Checked = prefs.BoolWithFallback(prefShowColFilters, false)

	// "Count play after" submenu: how much of a track must play to count as a play.
	curPct := prefs.IntWithFallback(prefPlayCountPct, defaultPlayCountPct)
	pctItem := func(label string, pct int) *fyne.MenuItem {
		it := fyne.NewMenuItem(label, func() { u.setPlayCountPct(pct) })
		it.Checked = curPct == pct
		return it
	}
	countAfterItem := fyne.NewMenuItem("Count play after", nil)
	countAfterItem.ChildMenu = fyne.NewMenu("",
		pctItem("25%", 25),
		pctItem("50%", 50),
		pctItem("75%", 75),
		pctItem("90%", 90),
		pctItem("End of track (100%)", 100),
	)

	prefItem := fyne.NewMenuItem("Preferences…", u.showPreferences)
	prefItem.Shortcut = &desktop.CustomShortcut{KeyName: fyne.KeyR, Modifier: fyne.KeyModifierAlt}

	viewMenu := fyne.NewMenu("View",
		prefItem,
		fyne.NewMenuItemSeparator(),
		hideItem,
		fyne.NewMenuItem("Show All Windows", u.showAllWindows),
		fyne.NewMenuItemSeparator(),
		trackColItem,
		fileColItem,
		durColItem,
		fmtColItem,
		brColItem,
		genreColItem,
		selColItem,
		colFilterItem,
		countAfterItem,
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Light Theme", func() { setLightTheme(a) }),
		fyne.NewMenuItem("Dark Theme", func() { setDarkTheme(a) }),
		fyne.NewMenuItem("System Theme", func() { setSystemTheme(a) }),
	)

	helpMenu := fyne.NewMenu("Help",
		fyne.NewMenuItem("Help", func() { showHelp(a) }),
		fyne.NewMenuItem("Check for Updates", func() {
			// Manual check is never throttled (minDays 0) but shares the cache file.
			msg, avail, remoteTag := updateChecker(updateRepoOwner, updateRepoName,
				updateRepoName, "", updateCheckStatePath(), 0)
			ahead := versionIsNewer(appVersion, remoteTag)
			showUpdateDialog(a, msg, avail, ahead)
		}),
		fyne.NewMenuItem("About", func() { showAbout(a) }),
	)

	return fyne.NewMainMenu(fileMenu, playbackMenu, playlistsMenu, viewMenu, helpMenu)
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
