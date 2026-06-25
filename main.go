package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/driver/desktop"

	"mediaplayer/internal/i18n"
)

const (
	// appName    = "KrankyBear MediaPlayer"
	appVersion = "1.0.2" // see FyneApp.toml
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
	langFlag := flag.String("lang", "", "UI language code (e.g. en, de); overrides the saved preference for this run")
	flag.Parse()

	a := app.NewWithID("com.github.amarillier.KrankyBearMediaPlayer")
	a.SetIcon(holidayAppIcon()) // holiday-themed bear on holidays, default otherwise
	setupI18n(a, *langFlag)     // load message catalog + resolve UI language before building any UI
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
	win.SetIcon(holidayAppIcon())
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

		fyne.NewMenuItem(i18n.T("tray.show_all"), func() { fyne.Do(u.showAllWindows) }),
		fyne.NewMenuItem(i18n.T("tray.hide_all"), func() { fyne.Do(u.hideAllWindows) }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem(i18n.T("tray.play_pause"), func() { fyne.Do(u.onPlayPause) }),
		fyne.NewMenuItem(i18n.T("tray.previous"), u.player.Prev),
		fyne.NewMenuItem(i18n.T("tray.next"), u.player.Next),
		fyne.NewMenuItem(i18n.T("tray.stop"), u.player.Stop),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem(i18n.T("tray.preferences"), func() { fyne.Do(u.showPreferences) }),
		fyne.NewMenuItemSeparator(),
		// Themes + About/Check for Updates/Help mirror the main menu for fast access
		// (reuse the menu.* keys so no extra translations are needed).
		fyne.NewMenuItem(i18n.T("menu.view.theme_dark"), func() { fyne.Do(func() { setDarkTheme(a) }) }),
		fyne.NewMenuItem(i18n.T("menu.view.theme_light"), func() { fyne.Do(func() { setLightTheme(a) }) }),
		fyne.NewMenuItem(i18n.T("menu.view.theme_system"), func() { fyne.Do(func() { setSystemTheme(a) }) }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem(i18n.T("menu.help.about"), func() { fyne.Do(func() { showAbout(a) }) }),
		fyne.NewMenuItem(i18n.T("menu.help.check_updates"), func() { u.checkForUpdatesManual() }), // handles its own threading
		fyne.NewMenuItem(i18n.T("menu.help.help"), func() { fyne.Do(func() { showHelp(a) }) }),

		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem(i18n.T("tray.quit"), func() { fyne.Do(u.quit) }), // tray runs off the main goroutine
	)
	desk.SetSystemTrayMenu(menu)
	desk.SetSystemTrayIcon(holidayAppIcon())
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

// macOSSpecialItems builds the menu entries for an item that macOS hoists into the
// application menu purely by its exact English label (see Fyne's menu_darwin.go:
// "About", "Preferences…", "Settings…"). Translating the label means Fyne no longer
// recognises it, so it would stay in the menu bar instead of the app menu.
//
// To get BOTH the native app-menu slot AND a localized entry, on darwin we return the
// English item (which Fyne moves to the app menu) plus — when the active locale differs
// — a translated copy that stays in the menu bar. Off darwin (no hoisting) we return
// just the translated item. An optional shortcut goes on the menu-bar-visible item.
func macOSSpecialItems(englishLabel, localized string, action func(), shortcut fyne.Shortcut) []*fyne.MenuItem {
	if runtime.GOOS != "darwin" {
		it := fyne.NewMenuItem(localized, action)
		it.Shortcut = shortcut
		return []*fyne.MenuItem{it}
	}
	hoisted := fyne.NewMenuItem(englishLabel, action) // Fyne moves this into the app menu
	if localized == englishLabel {
		hoisted.Shortcut = shortcut
		return []*fyne.MenuItem{hoisted}
	}
	bar := fyne.NewMenuItem(localized, action) // stays in the menu bar
	bar.Shortcut = shortcut
	return []*fyne.MenuItem{hoisted, bar}
}

// buildMenu builds the application main menu. Help/About/Update/Theme reuse the
// existing template dialogs; File/Library drive the catalog.
func buildMenu(a fyne.App, u *ui) *fyne.MainMenu {
	fileMenu := fyne.NewMenu(i18n.T("menu.library.label"),
		fyne.NewMenuItem(i18n.T("menu.library.add_folder"), u.addFolder),
		fyne.NewMenuItem(i18n.T("menu.library.manage_folders"), u.manageFolders),
		fyne.NewMenuItem(i18n.T("menu.library.rescan_all"), u.rescanAll),
		fyne.NewMenuItem(i18n.T("menu.library.relocate"), u.relocateFolder),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem(i18n.T("menu.library.select_all_shown"), u.selectAllShown),
		fyne.NewMenuItem(i18n.T("menu.library.clear_selection"), u.clearSelection),
		fyne.NewMenuItem(i18n.T("menu.library.copy_selected"), u.copySelectedTo),
		fyne.NewMenuItem(i18n.T("menu.library.edit_tags"), u.editTagsOfSelected),
		fyne.NewMenuItem(i18n.T("menu.library.rename_selected"), u.renameSelectedFromTags),
		fyne.NewMenuItem(i18n.T("menu.library.scan_replaygain"), u.scanReplayGainSelected),
		fyne.NewMenuItem(i18n.T("menu.library.add_selected_queue"), u.enqueueSelected),
		fyne.NewMenuItem(i18n.T("menu.library.delete_selected"), u.deleteSelectedFiles),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem(i18n.T("menu.library.library_report"), u.showLibraryReport),
		fyne.NewMenuItem(i18n.T("menu.library.add_all_queue"), u.enqueueShown),
		fyne.NewMenuItemSeparator(),
		// Defer via fyne.Do: quitting directly from the menu popup's click handler
		// hangs on Windows (closes the window from inside the popup callback).
		fyne.NewMenuItem(i18n.T("menu.library.quit"), func() { fyne.Do(u.quit) }),
	)

	hideItem := fyne.NewMenuItem(i18n.T("menu.view.hide_all"), u.hideAllWindows)
	hideItem.Shortcut = &desktop.CustomShortcut{KeyName: fyne.KeyH, Modifier: fyne.KeyModifierAlt}
	playPauseItem := fyne.NewMenuItem(i18n.T("menu.playback.play_pause"), u.onPlayPause)
	playPauseItem.Shortcut = &desktop.CustomShortcut{KeyName: fyne.KeyP, Modifier: fyne.KeyModifierAlt}
	prevItem := fyne.NewMenuItem(i18n.T("menu.playback.previous"), u.player.Prev)
	prevItem.Shortcut = &desktop.CustomShortcut{KeyName: fyne.KeyLeft, Modifier: fyne.KeyModifierAlt}
	nextItem := fyne.NewMenuItem(i18n.T("menu.playback.next"), u.player.Next)
	nextItem.Shortcut = &desktop.CustomShortcut{KeyName: fyne.KeyRight, Modifier: fyne.KeyModifierAlt}

	// Playback menu: transport plus shuffle/repeat (state persists via prefs).
	shuffleItem := fyne.NewMenuItem(i18n.T("menu.playback.shuffle"), u.toggleShuffle)
	shuffleItem.Checked = u.player.Shuffle()
	curRepeat := u.player.Repeat()
	repeatItemFor := func(label string, m RepeatMode) *fyne.MenuItem {
		it := fyne.NewMenuItem(label, func() { u.setRepeat(m) })
		it.Checked = curRepeat == m
		return it
	}
	repeatItem := fyne.NewMenuItem(i18n.T("menu.playback.repeat"), nil)
	repeatItem.ChildMenu = fyne.NewMenu("",
		repeatItemFor(i18n.T("menu.playback.repeat_off"), RepeatOff),
		repeatItemFor(i18n.T("menu.playback.repeat_all"), RepeatAll),
		repeatItemFor(i18n.T("menu.playback.repeat_one"), RepeatOne),
	)
	rgItem := fyne.NewMenuItem(i18n.T("menu.playback.replaygain"), u.toggleReplayGain)
	rgItem.Checked = a.Preferences().BoolWithFallback(prefReplayGain, false)
	rgAlbumItem := fyne.NewMenuItem(i18n.T("menu.playback.replaygain_album"), u.toggleReplayGainAlbum)
	rgAlbumItem.Checked = a.Preferences().BoolWithFallback(prefRGAlbum, false)

	curTrans := transitionMode(a.Preferences().IntWithFallback(prefTransition, int(transGap)))
	transItemFor := func(label string, m transitionMode) *fyne.MenuItem {
		it := fyne.NewMenuItem(label, func() { u.setTransition(m) })
		it.Checked = curTrans == m
		return it
	}
	transitionItem := fyne.NewMenuItem(i18n.T("menu.playback.transition"), nil)
	transitionItem.ChildMenu = fyne.NewMenu("",
		transItemFor(i18n.T("menu.playback.transition_gap"), transGap),
		transItemFor(i18n.T("menu.playback.transition_gapless"), transGapless),
		transItemFor(i18n.T("menu.playback.transition_crossfade"), transCrossfade),
	)

	playbackMenu := fyne.NewMenu(i18n.T("menu.playback.label"),
		playPauseItem,
		prevItem,
		nextItem,
		fyne.NewMenuItem(i18n.T("menu.playback.stop"), u.player.Stop),
		fyne.NewMenuItemSeparator(),
		shuffleItem,
		repeatItem,
		rgItem,
		rgAlbumItem,
		fyne.NewMenuItem(i18n.T("menu.playback.equalizer"), u.showEqualizer),
		transitionItem,
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem(i18n.T("menu.playback.show_queue"), u.showQueue),
	)

	// Playlists menu: static .m3u8 files plus dynamic ("smart") playlists. Saved
	// smart playlists are listed inline so a click applies them.
	playlistItems := []*fyne.MenuItem{
		fyne.NewMenuItem(i18n.T("menu.playlists.open"), u.openPlaylist),
		fyne.NewMenuItem(i18n.T("menu.playlists.save"), u.savePlaylist),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem(i18n.T("menu.playlists.new_smart"), u.showNewSmartPlaylist),
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
			fyne.NewMenuItem(i18n.T("menu.playlists.manage_smart"), u.manageSmartPlaylists))
	}
	playlistsMenu := fyne.NewMenu(i18n.T("menu.playlists.label"), playlistItems...)

	// Checkable toggles for the optional columns (state persists via prefs).
	prefs := a.Preferences()
	trackColItem := fyne.NewMenuItem(i18n.T("menu.view.col_track"), func() { u.toggleColumn(prefShowTrackCol) })
	trackColItem.Checked = prefs.BoolWithFallback(prefShowTrackCol, false)
	fileColItem := fyne.NewMenuItem(i18n.T("menu.view.col_filename"), func() { u.toggleColumn(prefShowFilenameCol) })
	fileColItem.Checked = prefs.BoolWithFallback(prefShowFilenameCol, false)
	selColItem := fyne.NewMenuItem(i18n.T("menu.view.col_select"), func() { u.toggleColumn(prefShowSelectCol) })
	selColItem.Checked = prefs.BoolWithFallback(prefShowSelectCol, false)
	durColItem := fyne.NewMenuItem(i18n.T("menu.view.col_length"), func() { u.toggleColumn(prefShowDurationCol) })
	durColItem.Checked = prefs.BoolWithFallback(prefShowDurationCol, true)
	fmtColItem := fyne.NewMenuItem(i18n.T("menu.view.col_format"), func() { u.toggleColumn(prefShowFormatCol) })
	fmtColItem.Checked = prefs.BoolWithFallback(prefShowFormatCol, false)
	brColItem := fyne.NewMenuItem(i18n.T("menu.view.col_bitrate"), func() { u.toggleColumn(prefShowBitrateCol) })
	brColItem.Checked = prefs.BoolWithFallback(prefShowBitrateCol, false)
	genreColItem := fyne.NewMenuItem(i18n.T("menu.view.col_genre"), func() { u.toggleColumn(prefShowGenreCol) })
	genreColItem.Checked = prefs.BoolWithFallback(prefShowGenreCol, false)
	colFilterItem := fyne.NewMenuItem(i18n.T("menu.view.col_filters"), u.toggleColumnFilters)
	colFilterItem.Checked = prefs.BoolWithFallback(prefShowColFilters, false)

	// "Count play after" submenu: how much of a track must play to count as a play.
	curPct := prefs.IntWithFallback(prefPlayCountPct, defaultPlayCountPct)
	pctItem := func(label string, pct int) *fyne.MenuItem {
		it := fyne.NewMenuItem(label, func() { u.setPlayCountPct(pct) })
		it.Checked = curPct == pct
		return it
	}
	countAfterItem := fyne.NewMenuItem(i18n.T("menu.view.count_play_after"), nil)
	countAfterItem.ChildMenu = fyne.NewMenu("",
		pctItem("25%", 25),
		pctItem("50%", 50),
		pctItem("75%", 75),
		pctItem("90%", 90),
		pctItem(i18n.T("menu.view.count_end"), 100),
	)

	// Preferences: macOS hoists the English-labelled item into the app menu; on
	// darwin a translated copy also stays in the View menu (see macOSSpecialItems).
	prefShortcut := &desktop.CustomShortcut{KeyName: fyne.KeyR, Modifier: fyne.KeyModifierAlt}
	viewItems := macOSSpecialItems("Preferences…", i18n.T("menu.view.preferences"), u.showPreferences, prefShortcut)
	viewItems = append(viewItems,
		fyne.NewMenuItemSeparator(),
		hideItem,
		fyne.NewMenuItem(i18n.T("menu.view.show_all"), u.showAllWindows),
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
		fyne.NewMenuItem(i18n.T("menu.view.theme_light"), func() { setLightTheme(a) }),
		fyne.NewMenuItem(i18n.T("menu.view.theme_dark"), func() { setDarkTheme(a) }),
		fyne.NewMenuItem(i18n.T("menu.view.theme_system"), func() { setSystemTheme(a) }),
	)
	viewMenu := fyne.NewMenu(i18n.T("menu.view.label"), viewItems...)

	helpItems := []*fyne.MenuItem{
		fyne.NewMenuItem(i18n.T("menu.help.help"), func() { showHelp(a) }),
		fyne.NewMenuItem(i18n.T("menu.help.check_updates"), func() {
			// Runs the network check off the UI thread (see checkForUpdatesManual);
			// doing it inline here froze the window with a spinning cursor.
			u.checkForUpdatesManual()
		}),
	}
	// About: same macOS app-menu hoisting as Preferences (no shortcut).
	helpItems = append(helpItems, macOSSpecialItems("About", i18n.T("menu.help.about"), func() { showAbout(a) }, nil)...)
	helpMenu := fyne.NewMenu(i18n.T("menu.help.label"), helpItems...)

	return fyne.NewMainMenu(fileMenu, playbackMenu, playlistsMenu, viewMenu, helpMenu)
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
