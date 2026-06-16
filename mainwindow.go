// Package main - mainwindow.go builds the library window: a spreadsheet-style
// table of catalogued tracks (with embedded album-art thumbnails), a rating
// filter, folder management, and the playback transport.
package main

import (
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	fynetooltip "github.com/dweymouth/fyne-tooltip"
	ttwidget "github.com/dweymouth/fyne-tooltip/widget"
)

// Logical column ids. These are stable identities used for rendering, sorting
// and toggling - distinct from a column's *physical* position in the table,
// which changes as optional columns are shown/hidden (see visibleCols).
const (
	colArt = iota
	colTrack
	colTitle
	colArtist
	colAlbum
	colYear
	colPlays
	colRating
	colDuration // playback length (filled in by the background enricher)
	colFormat   // file type (from extension)
	colBitrate  // average kbps (derived from size + duration)
	colFilename
	colSelect // checkbox column for marking tracks (copy to media)
	colGenre  // optional Genre column (appended last to keep the ids above stable)
)

// columnDef describes one library column. text==nil marks the Art column (an
// image cell); optional columns can be toggled on/off from the View menu.
type columnDef struct {
	id       int
	title    string
	width    float32
	optional bool
	text     func(tr Track) string
}

// allColumns lists every column in display order. Optional ones (Track #,
// Filename) default off to keep the view uncluttered.
var allColumns = []columnDef{
	{colSelect, "✓", 44, true, nil}, // marking checkbox (rendered specially); leftmost
	{colArt, "Art", 56, false, nil},
	{colTrack, "#", 44, true, func(tr Track) string {
		if tr.TrackNo > 0 {
			return fmt.Sprintf("%d", tr.TrackNo)
		}
		return ""
	}},
	{colTitle, "Title", 240, false, func(tr Track) string { return tr.Title }},
	{colArtist, "Artist", 170, false, func(tr Track) string { return tr.Artist }},
	{colAlbum, "Album", 190, false, func(tr Track) string { return tr.Album }},
	{colGenre, "Genre", 120, true, func(tr Track) string { return tr.Genre }},
	{colYear, "Year", 56, false, func(tr Track) string {
		if tr.Year > 0 {
			return fmt.Sprintf("%d", tr.Year)
		}
		return ""
	}},
	{colPlays, "Plays", 64, false, func(tr Track) string { return fmt.Sprintf("%d", tr.PlayCount) }},
	{colRating, "Rating", 96, false, func(tr Track) string { return starString(tr.EffectiveRating()) }},
	{colDuration, "Length", 64, true, func(tr Track) string {
		if tr.Duration > 0 {
			return fmtDuration(time.Duration(tr.Duration) * time.Second)
		}
		return ""
	}},
	{colFormat, "Format", 64, true, func(tr Track) string { return trackFormat(tr) }},
	{colBitrate, "Bitrate", 72, true, func(tr Track) string {
		if kbps := trackBitrate(tr); kbps > 0 {
			return fmt.Sprintf("%d kbps", kbps)
		}
		return ""
	}},
	{colFilename, "Filename", 220, true, func(tr Track) string { return filepath.Base(tr.RelPath) }},
}

// trackFormat is the upper-cased file type from the extension (e.g. "MP3").
func trackFormat(tr Track) string {
	ext := strings.TrimPrefix(filepath.Ext(tr.RelPath), ".")
	return strings.ToUpper(ext)
}

// trackBitrate is the average bitrate in kbps, derived from file size and
// duration (0 until the duration enricher has run). Approximate for VBR, and it
// counts tag/art overhead, but it's a useful at-a-glance quality indicator.
func trackBitrate(tr Track) int {
	if tr.Duration <= 0 || tr.FileSize <= 0 {
		return 0
	}
	return int(tr.FileSize * 8 / int64(tr.Duration) / 1000)
}

// Preference keys for the optional columns (remembered across launches).
const (
	prefShowTrackCol    = "showTrackColumn"
	prefShowFilenameCol = "showFilenameColumn"
	prefShowSelectCol   = "showSelectColumn"
	prefShowDurationCol = "showDurationColumn"
	prefShowFormatCol   = "showFormatColumn"
	prefShowBitrateCol  = "showBitrateColumn"
	prefShowGenreCol    = "showGenreColumn"
	prefShowColFilters  = "showColumnFilters"
)

// Preference keys for restoring the session on relaunch: the sort sequence and
// the last-played track (selected + scrolled into view, never auto-played).
const (
	prefSortCol     = "sortCol"
	prefSortDesc    = "sortDesc"
	prefSortCol2    = "sortCol2"
	prefSortDesc2   = "sortDesc2"
	prefLastTrackID = "lastTrackID"
)

// prefPlayCountPct is the percentage of a track that must play before it counts
// as a play (and bumps the auto rating). 100 = only at natural end.
const prefPlayCountPct = "playCountPercent"
const defaultPlayCountPct = 50

// Playback mode preferences (remembered across launches).
const prefShuffle = "shuffle"       // bool
const prefRepeat = "repeatMode"     // int: 0 off, 1 all, 2 one
const prefReplayGain = "replayGain" // bool: apply per-track ReplayGain
const prefTransition = "transition" // int: 0 gap, 1 gapless, 2 crossfade
const prefVolume = "volume"         // float 0..1 linear gain

// prefRenamePattern / prefParsePattern are the last-used filename patterns for the
// rename-from-tags and tags-from-filename tools (rename.go, tageditor.go).
const prefRenamePattern = "renamePattern"
const prefParsePattern = "parsePattern"

// prefDBPath is the saved custom catalog-database path (empty = default location).
const prefDBPath = "dbPath"

const tableRowHeight = 52

// maxArtBytes caps how much image data we read when adding album art (file or
// URL), so a huge/hostile image can't exhaust memory.
const maxArtBytes = 25 << 20 // 25 MB

// ui holds the live widgets and state of the main window.
type ui struct {
	app    fyne.App
	win    fyne.Window
	db     *DB
	player *Player

	tracks  []Track
	filter  Filter
	search  string
	qGenre  string // active smart-playlist constraints (empty = none)
	qArtist string
	qAlbum  string
	// Per-column filter row (toggle in View). Empty = no constraint; fYear is a
	// GLOB pattern (see db.TrackQuery). Transient - not persisted across launches.
	fTitle       string
	fArtist      string
	fAlbum       string
	fGenre       string
	fYear        string
	fPlays       string
	smartName    string // name of the applied smart playlist, "" if none
	sortCol      int    // primary sort column, -1 = default order
	sortDesc     bool   // primary descending
	sortCol2     int    // secondary sort column (shift-click), -1 = none
	sortDesc2    bool   // secondary descending
	selected     int    // selected table row, -1 if none
	nowPlayingID int64  // track id last followed by the selection indicator

	marked map[int64]markEntry // tracks ticked for copy: id -> path+artist+album

	table             *widget.Table
	visibleCols       []columnDef // currently shown columns, in physical order
	thumbCache        map[int64]fyne.Resource
	colFilterRow      fyne.CanvasObject // the toggleable per-column filter row
	colFilterBox      []*widget.Entry   // its entries, for Clear / hide-clearing
	colFilterClearing bool              // true while clearing filters, to coalesce into one reload

	nowPlaying *tappableLabel
	nowArt     *canvas.Image
	playPause  *ttwidget.Button
	status     *widget.Label

	seekSlider *widget.Slider
	timeLabel  *widget.Label
	seeking    bool           // true while the user drags the seek slider
	volSlider  *widget.Slider // per-stream playback gain (player.go)

	// System output volume controls (sysvolume.go), distinct from the gain
	// above: these move the whole OS output level. A background poll keeps them
	// in sync with external changes (keyboard volume keys, OS mixer).
	sysVolSlider   *widget.Slider
	sysMuteBtn     *ttwidget.Button
	sysMuted       bool
	sysVolApplying bool      // true while we set the slider from a poll, so its OnChangeEnded doesn't echo back to the OS
	sysVolTouched  time.Time // when the user last moved the slider; the poll backs off until they settle

	// Play Queue window state (see queue.go).
	queueList    *widget.List
	queueTracks  []Track // queued tracks in play order (snapshot for the list)
	queueCurrent int     // play-order position of the now-playing track, -1 if none
	queueSel     int     // selected row in the queue list, -1 if none

	tickerDone chan struct{} // closed to stop the progress ticker (on quit)
	tickerOnce sync.Once     // guards closing tickerDone exactly once

	enriching atomic.Bool // true while the background duration enricher is running (enrich.go)
}

// tappableLabel is a Label that runs onTap when clicked and shows a pointer
// cursor on hover. Used for the now-playing track name in the transport bar, so
// clicking it jumps the table to the currently-playing track.
type tappableLabel struct {
	widget.Label
	onTap func()
}

func newTappableLabel(onTap func()) *tappableLabel {
	l := &tappableLabel{onTap: onTap}
	l.ExtendBaseWidget(l)
	return l
}

func (l *tappableLabel) Tapped(_ *fyne.PointEvent) {
	if l.onTap != nil {
		l.onTap()
	}
}

func (l *tappableLabel) Cursor() desktop.Cursor { return desktop.PointerCursor }

// buildMainWindow constructs and populates the main window content.
func buildMainWindow(a fyne.App, win fyne.Window, db *DB, player *Player) *ui {
	u := &ui{
		app:          a,
		win:          win,
		db:           db,
		player:       player,
		filter:       FilterAll,
		sortCol:      -1,
		sortCol2:     -1,
		selected:     -1,
		marked:       map[int64]markEntry{},
		thumbCache:   map[int64]fyne.Resource{},
		tickerDone:   make(chan struct{}),
		queueCurrent: -1,
		queueSel:     -1,
	}

	u.table = u.buildTable()
	u.rebuildColumns() // populate visibleCols + column widths from prefs
	top := u.buildToolbar()
	u.colFilterRow = u.buildColumnFilters()
	if !a.Preferences().BoolWithFallback(prefShowColFilters, false) {
		u.colFilterRow.Hide() // off by default; toggle via View → Show Column Filters
	}
	bottom := u.buildTransport()

	content := container.NewBorder(container.NewVBox(top, u.colFilterRow), bottom, nil, nil, u.table)
	// Wrap in a tooltip layer so the ttwidget buttons' tips render (see button
	// SetToolTip calls). Torn down in quit().
	win.SetContent(fynetooltip.AddWindowToolTipLayer(content, win.Canvas()))

	// Refresh the transport when playback state changes (off the UI thread).
	player.OnChange = func() { fyne.Do(u.refreshNowPlaying) }
	// When a play is credited, update the in-memory count live (on the UI thread).
	player.OnPlayCounted = func(id int64) { fyne.Do(func() { u.onPlayCounted(id) }) }

	// Apply the saved play-count threshold and playback modes.
	prefs := u.app.Preferences()
	pct := prefs.IntWithFallback(prefPlayCountPct, defaultPlayCountPct)
	player.SetCountThreshold(float64(pct) / 100)
	player.SetShuffle(prefs.BoolWithFallback(prefShuffle, false))
	player.SetRepeat(RepeatMode(prefs.IntWithFallback(prefRepeat, int(RepeatOff))))
	player.SetReplayGain(prefs.BoolWithFallback(prefReplayGain, false))
	player.SetTransitionMode(transitionMode(prefs.IntWithFallback(prefTransition, int(transGap))))
	vol := prefs.FloatWithFallback(prefVolume, 1.0)
	player.SetVolume(vol)
	u.volSlider.SetValue(vol) // reflect the restored volume in the slider

	// Restore the saved sort sequence before the first load so the table opens
	// ordered as the user left it.
	u.sortCol = prefs.IntWithFallback(prefSortCol, -1)
	u.sortDesc = prefs.BoolWithFallback(prefSortDesc, false)
	u.sortCol2 = prefs.IntWithFallback(prefSortCol2, -1)
	u.sortDesc2 = prefs.BoolWithFallback(prefSortDesc2, false)

	u.reload()
	u.startProgressTicker()
	u.startSystemVolumePoll()
	u.startDurationEnricher() // fill in any missing track lengths in the background
	u.restoreLastTrack()      // select + scroll to the track last playing (no audio)
	return u
}

// onPlayCounted bumps the displayed play count (and thus the auto star rating)
// for a track the moment it's credited, without a full reload. Runs on the UI
// thread. The DB increment itself is done by the player.
func (u *ui) onPlayCounted(id int64) {
	for i := range u.tracks {
		if u.tracks[i].ID == id {
			u.tracks[i].PlayCount++
			break
		}
	}
	u.table.Refresh()
}

// rebuildMenu rebuilds the main menu, deferred via fyne.Do (see toggleColumn for
// the macOS menu-tracking reason). Used after state that the menu reflects
// changes (toggles, smart-playlist list, etc.).
func (u *ui) rebuildMenu() {
	fyne.Do(func() { u.win.SetMainMenu(buildMenu(u.app, u)) })
}

// setPlayCountPct saves the play-count threshold preference and applies it. The
// menu is rebuilt via fyne.Do (see toggleColumn for why - macOS menu tracking).
func (u *ui) setPlayCountPct(pct int) {
	u.app.Preferences().SetInt(prefPlayCountPct, pct)
	u.player.SetCountThreshold(float64(pct) / 100)
	fyne.Do(func() { u.win.SetMainMenu(buildMenu(u.app, u)) })
}

// toggleShuffle flips shuffle, persists it, and rebuilds the menu to update the
// checkmark (deferred via fyne.Do - see toggleColumn for the macOS reason).
func (u *ui) toggleShuffle() {
	on := !u.player.Shuffle()
	u.player.SetShuffle(on)
	u.app.Preferences().SetBool(prefShuffle, on)
	fyne.Do(func() { u.win.SetMainMenu(buildMenu(u.app, u)) })
}

// setRepeat sets the repeat mode, persists it, and rebuilds the menu.
func (u *ui) setRepeat(m RepeatMode) {
	u.player.SetRepeat(m)
	u.app.Preferences().SetInt(prefRepeat, int(m))
	fyne.Do(func() { u.win.SetMainMenu(buildMenu(u.app, u)) })
}

// setTransition sets the track-transition mode (gap/gapless/crossfade), persists
// it, and rebuilds the menu radio-checkmarks.
func (u *ui) setTransition(m transitionMode) {
	u.player.SetTransitionMode(m)
	u.app.Preferences().SetInt(prefTransition, int(m))
	fyne.Do(func() { u.win.SetMainMenu(buildMenu(u.app, u)) })
}

// toggleReplayGain flips ReplayGain volume normalization, persists it, applies it
// to the current track, and rebuilds the menu checkmark.
func (u *ui) toggleReplayGain() {
	on := !u.app.Preferences().BoolWithFallback(prefReplayGain, false)
	u.player.SetReplayGain(on)
	u.app.Preferences().SetBool(prefReplayGain, on)
	fyne.Do(func() { u.win.SetMainMenu(buildMenu(u.app, u)) })
}

// startProgressTicker polls playback position and updates the seek slider/time
// label. The poll runs on its own goroutine, so all widget access is marshalled
// onto the UI thread with fyne.Do (Fyne requires UI updates on the main thread).
func (u *ui) startProgressTicker() {
	go func() {
		t := time.NewTicker(500 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-u.tickerDone:
				return // stop cleanly on quit - no more fyne.Do after teardown
			case <-t.C:
				fyne.Do(u.updateProgress)
			}
		}
	}()
}

// stopTicker halts the progress ticker (idempotent). Must run before quitting so
// it can't enqueue fyne.Do(updateProgress) calls that touch widgets during/after
// window teardown - a source of quit hangs.
func (u *ui) stopTicker() {
	u.tickerOnce.Do(func() { close(u.tickerDone) })
}

// quit performs the full, ordered teardown and exits. Mirrors the proven
// TaniumQuest sequence: stop background work, persist window size, stop audio,
// then App.Quit(). Call directly on the main thread (close-intercept / main
// menu); from the system tray (off the main goroutine) wrap it in fyne.Do.
func (u *ui) quit() {
	u.stopTicker()
	fynetooltip.DestroyWindowToolTipLayer(u.win.Canvas())
	saveMainWindowGeometry(u.app, u.win)
	u.player.Stop()
	u.app.Quit()
}

// updateProgress refreshes the seek slider + time label. Runs on the UI thread.
func (u *ui) updateProgress() {
	u.player.MaybeCountPlay() // credit a play once it passes the % threshold
	pos, total := u.player.Progress()
	if total <= 0 {
		u.timeLabel.SetText("--:-- / --:--")
		if !u.seeking {
			u.seekSlider.Value = 0
			u.seekSlider.Refresh()
		}
		return
	}
	u.timeLabel.SetText(fmt.Sprintf("%s / %s", fmtDuration(pos), fmtDuration(total)))
	if !u.seeking {
		// Set Value directly (not SetValue) so we don't fire OnChanged -> seek.
		u.seekSlider.Value = pos.Seconds() / total.Seconds() * 100
		u.seekSlider.Refresh()
	}
}

// fmtDuration renders a duration as m:ss (or h:mm:ss for long tracks).
func fmtDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// buildToolbar builds the top bar: add/rescan folders + rating filter.
func (u *ui) buildToolbar() fyne.CanvasObject {
	addBtn := ttwidget.NewButtonWithIcon("Add Folder", theme.FolderOpenIcon(), u.addFolder)
	addBtn.SetToolTip("Add a folder to the library and scan it")
	rescanBtn := ttwidget.NewButtonWithIcon("Rescan", theme.ViewRefreshIcon(), u.rescanAll)
	rescanBtn.SetToolTip("Rescan watched folders for new, changed or removed files")
	// "Now Playing" jumps the list to the current track. A music-note icon (not a
	// play triangle) and a label make clear it locates the track, doesn't play it.
	jumpBtn := ttwidget.NewButtonWithIcon("Now Playing", theme.MediaMusicIcon(), u.jumpToCurrent)
	jumpBtn.Importance = widget.LowImportance
	jumpBtn.SetToolTip("Scroll to and select the track that's playing (doesn't start playback)")

	filterSel := widget.NewSelect(filterLabels(), nil)
	// Set the default selection before wiring the callback so we don't trigger
	// a reload mid-construction (other widgets aren't built yet).
	filterSel.SetSelectedIndex(0)
	filterSel.OnChanged = func(label string) {
		u.filter = filterByLabel(label)
		u.clearSmartCriteria() // a manual filter change leaves any smart playlist
		u.reload()
	}

	left := container.NewHBox(addBtn, rescanBtn, jumpBtn, widget.NewSeparator(),
		widget.NewLabel("Show:"), filterSel)

	// Search across title/artist/album/genre/filename (handled in SQL).
	search := widget.NewEntry()
	search.SetPlaceHolder("Search title, artist, album, genre, filename…")
	search.OnChanged = func(s string) {
		u.search = s
		u.clearSmartCriteria() // typing a search leaves any smart playlist
		u.reload()
	}
	clearSearch := ttwidget.NewButtonWithIcon("", theme.ContentClearIcon(), func() {
		search.SetText("") // fires OnChanged -> reload
	})
	clearSearch.SetToolTip("Clear the search box")

	// Search box expands to fill the width between the controls and the button.
	searchRow := container.NewBorder(nil, nil, left, clearSearch, search)
	return container.NewVBox(searchRow, widget.NewSeparator())
}

// buildColumnFilters builds the toggleable per-column filter row: a labelled set
// of small entries that narrow the table live - substring match on text columns,
// a GLOB pattern on Year (e.g. "202[456]" -> 2024-2026, "20*" -> the 2000s). All
// are ANDed with the search box and any rating filter / smart playlist. Hidden by
// default (View → Show Column Filters); hiding it clears the filters so nothing
// filters invisibly.
func (u *ui) buildColumnFilters() fyne.CanvasObject {
	mk := func(placeholder string, set func(string)) *widget.Entry {
		e := widget.NewEntry()
		e.SetPlaceHolder(placeholder)
		e.OnChanged = func(s string) {
			set(s)
			if !u.colFilterClearing {
				u.reload()
			}
		}
		return e
	}
	tE := mk("Title", func(s string) { u.fTitle = s })
	aE := mk("Artist", func(s string) { u.fArtist = s })
	alE := mk("Album", func(s string) { u.fAlbum = s })
	gE := mk("Genre", func(s string) { u.fGenre = s })
	yE := mk("Year e.g. 202[456]", func(s string) { u.fYear = s })
	pE := mk("Plays e.g. 5", func(s string) { u.fPlays = s })
	u.colFilterBox = []*widget.Entry{tE, aE, alE, gE, yE, pE}

	clear := ttwidget.NewButtonWithIcon("", theme.ContentClearIcon(), u.clearColumnFilters)
	clear.SetToolTip("Clear all column filters")
	wrap := func(w float32, e *widget.Entry) fyne.CanvasObject {
		return container.NewGridWrap(fyne.NewSize(w, e.MinSize().Height), e)
	}
	row := container.NewHBox(
		widget.NewLabel("Filter:"),
		wrap(150, tE), wrap(150, aE), wrap(150, alE), wrap(120, gE), wrap(140, yE), wrap(110, pE),
		clear,
	)
	return container.NewVBox(row, widget.NewSeparator())
}

// clearColumnFilters empties the filter row (and reloads once). Coalesces the
// per-entry reloads via colFilterClearing.
func (u *ui) clearColumnFilters() {
	u.colFilterClearing = true
	for _, e := range u.colFilterBox {
		e.SetText("")
	}
	u.colFilterClearing = false
	u.reload()
}

// applyColFilterVisibility shows or hides the filter row per the saved preference,
// clearing the filters when hiding so they don't keep narrowing the table unseen.
func (u *ui) applyColFilterVisibility() {
	if u.app.Preferences().BoolWithFallback(prefShowColFilters, false) {
		u.colFilterRow.Show()
	} else {
		u.colFilterRow.Hide()
		u.clearColumnFilters()
	}
}

// toggleColumnFilters flips the filter-row preference and applies it. The menu is
// rebuilt via fyne.Do for its checkmark (see toggleColumn for the macOS reason).
func (u *ui) toggleColumnFilters() {
	prefs := u.app.Preferences()
	prefs.SetBool(prefShowColFilters, !prefs.BoolWithFallback(prefShowColFilters, false))
	fyne.Do(func() {
		u.applyColFilterVisibility()
		u.win.SetMainMenu(buildMenu(u.app, u))
	})
}

// buildTransport builds the bottom bar: now-playing, transport, rating setter.
func (u *ui) buildTransport() fyne.CanvasObject {
	u.nowArt = canvas.NewImageFromResource(resourceKrankyBearMediaPlayerPng)
	u.nowArt.FillMode = canvas.ImageFillContain
	u.nowArt.SetMinSize(fyne.NewSize(40, 40))

	u.nowPlaying = newTappableLabel(u.jumpToCurrent) // click the track name to jump to it
	u.nowPlaying.Wrapping = fyne.TextTruncate
	u.nowPlaying.SetText("Nothing playing")

	prev := ttwidget.NewButtonWithIcon("", theme.MediaSkipPreviousIcon(), u.player.Prev)
	prev.SetToolTip("Previous track (Alt+←)")
	u.playPause = ttwidget.NewButtonWithIcon("", theme.MediaPlayIcon(), u.onPlayPause)
	u.playPause.SetToolTip("Play / Pause (Alt+P)")
	stop := ttwidget.NewButtonWithIcon("", theme.MediaStopIcon(), u.player.Stop)
	stop.SetToolTip("Stop")
	next := ttwidget.NewButtonWithIcon("", theme.MediaSkipNextIcon(), u.player.Next)
	next.SetToolTip("Next track (Alt+→)")
	transport := container.NewHBox(prev, u.playPause, stop, next)

	u.status = widget.NewLabel("")

	// Seek bar (0..100% of the current track) + elapsed/total time.
	u.timeLabel = widget.NewLabel("--:-- / --:--")
	u.seekSlider = widget.NewSlider(0, 100)
	u.seekSlider.Step = 1
	// While dragging, pause the ticker's writes so it doesn't fight the user.
	u.seekSlider.OnChanged = func(float64) { u.seeking = true }
	u.seekSlider.OnChangeEnded = func(v float64) {
		u.player.Seek(v / 100)
		u.seeking = false
	}
	seekRow := container.NewBorder(nil, nil, nil, u.timeLabel, u.seekSlider)

	// Per-stream gain slider (0..1 linear), fixed width. Attenuates only this
	// app's output; see player.SetVolume.
	u.volSlider = widget.NewSlider(0, 1)
	u.volSlider.Step = 0.01
	u.volSlider.Value = u.player.Volume()
	u.volSlider.OnChanged = func(v float64) {
		u.player.SetVolume(v)
		u.app.Preferences().SetFloat(prefVolume, v) // remember across launches
	}
	volBox := container.NewBorder(nil, nil,
		container.NewHBox(widget.NewLabel("App"), widget.NewIcon(theme.VolumeUpIcon())), nil,
		container.NewGridWrap(fyne.NewSize(100, 28), u.volSlider))

	sysVolBox := u.buildSystemVolume()

	right := container.NewHBox(volBox, sysVolBox, widget.NewSeparator(), u.buildRatingBar())
	nowBox := container.NewBorder(nil, nil, u.nowArt, nil, u.nowPlaying)
	controls := container.NewBorder(nil, nil, transport, right, nowBox)

	// The KrankyBear logo, always visible at the bottom-left. It sits left of the
	// transport block (seek bar / controls / status), nudging those toward the
	// volume controls. Contained at ~128px so it's clearly visible without eating
	// much width.
	logo := newTappableImage(resourceKrankyBearMediaPlayerPng, fyne.NewSize(128, 128),
		func() { showEasterEgg(u.app, "🐻 You poked the bear!") })

	bottom := container.NewVBox(widget.NewSeparator(), seekRow, controls, u.status)
	return container.NewBorder(nil, nil, container.NewPadded(logo), nil, bottom)
}

// buildSystemVolume builds the OS output-volume control (sysvolume_*.go): a mute
// toggle plus a 0..100 slider that drives the whole system output level,
// distinct from the per-stream "App" gain. Every change is applied off the UI
// goroutine, and the slider sets the level on OnChangeEnded rather than
// OnChanged so a drag doesn't fire one OS volume call per tick. The initial
// value and ongoing sync with external changes are handled by
// startSystemVolumePoll.
func (u *ui) buildSystemVolume() fyne.CanvasObject {
	u.sysVolSlider = widget.NewSlider(0, 100)
	u.sysVolSlider.Step = 1
	// Record when the user is driving the slider so the poll won't fight them.
	// (SetValue also fires OnChanged, hence the sysVolApplying guard.)
	u.sysVolSlider.OnChanged = func(float64) {
		if !u.sysVolApplying {
			u.sysVolTouched = time.Now()
		}
	}
	u.sysVolSlider.OnChangeEnded = func(v float64) {
		if u.sysVolApplying {
			return // value came from a poll, not the user - don't echo it back to the OS
		}
		u.sysVolTouched = time.Now()
		pct := int(v + 0.5)
		go func() {
			if err := sysVolumeSet(pct); err != nil {
				log.Printf("system volume: set %d%% failed: %v", pct, err)
			}
		}()
	}

	u.sysMuteBtn = ttwidget.NewButtonWithIcon("", theme.VolumeUpIcon(), func() {
		u.setSystemMuted(!u.sysMuted)
	})
	u.sysMuteBtn.Importance = widget.LowImportance
	u.sysMuteBtn.SetToolTip("Mute / unmute the computer's output (system volume)")

	return container.NewBorder(nil, nil, widget.NewLabel("Sys"), u.sysMuteBtn,
		container.NewGridWrap(fyne.NewSize(100, 28), u.sysVolSlider))
}

// sysVolPollInterval is how often the Sys slider is reconciled with the OS
// output level. Each tick does two lightweight OS reads (a subprocess on macOS,
// a COM call on Windows); 2s keeps the slider current without measurable cost.
const sysVolPollInterval = 2 * time.Second

// startSystemVolumePoll reflects the OS volume/mute in the Sys control at
// startup and then polls so external changes (volume keys, the OS mixer) keep
// the slider in sync. It shares the progress ticker's shutdown channel, so it
// stops cleanly on quit (no fyne.Do after teardown).
func (u *ui) startSystemVolumePoll() {
	go func() {
		u.syncSystemVolume() // reflect the current state immediately
		t := time.NewTicker(sysVolPollInterval)
		defer t.Stop()
		for {
			select {
			case <-u.tickerDone:
				return
			case <-t.C:
				u.syncSystemVolume()
			}
		}
	}()
}

// syncSystemVolume reads the OS volume/mute (off the UI thread, since the reads
// are slow) and reflects any external change in the slider/mute button. It
// never fights an in-progress user interaction and never echoes the read value
// back to the OS. Errors (e.g. no audio device) are ignored: the control simply
// stays put until the OS recovers.
func (u *ui) syncSystemVolume() {
	vol, verr := sysVolumeGet()
	muted, merr := sysMutedGet()
	if verr != nil && merr != nil {
		return
	}
	fyne.Do(func() {
		// Leave the control alone if the user moved it within the last poll
		// window - their input wins until it settles and the OS catches up.
		if time.Since(u.sysVolTouched) < sysVolPollInterval {
			return
		}
		if verr == nil && int(u.sysVolSlider.Value+0.5) != vol {
			u.sysVolApplying = true
			u.sysVolSlider.SetValue(float64(vol))
			u.sysVolApplying = false
		}
		if merr == nil && muted != u.sysMuted {
			u.applyMuteIcon(muted)
		}
	})
}

// setSystemMuted applies a new OS mute state off the UI goroutine, updating the
// button icon immediately so the control feels responsive.
func (u *ui) setSystemMuted(muted bool) {
	u.applyMuteIcon(muted)
	go func() {
		if err := sysSetMuted(muted); err != nil {
			log.Printf("system volume: set muted=%v failed: %v", muted, err)
		}
	}()
}

// applyMuteIcon records the mute state and swaps the toggle icon to match. Must
// run on the UI goroutine.
func (u *ui) applyMuteIcon(muted bool) {
	u.sysMuted = muted
	if muted {
		u.sysMuteBtn.SetIcon(theme.VolumeMuteIcon())
	} else {
		u.sysMuteBtn.SetIcon(theme.VolumeUpIcon())
	}
}

// buildRatingBar builds the manual star-rating setter for the selected track.
func (u *ui) buildRatingBar() fyne.CanvasObject {
	btns := container.NewHBox(widget.NewLabel("Rate:"))
	for n := 1; n <= 5; n++ {
		star := n
		btns.Add(widget.NewButton(fmt.Sprintf("%d★", star), func() {
			u.rateSelected(star)
		}))
	}
	btns.Add(widget.NewButtonWithIcon("", theme.ContentClearIcon(), func() {
		u.rateSelected(0) // clear manual rating
	}))
	return btns
}

// rebuildColumns recomputes the visible column set from preferences and applies
// the column widths. Called at startup and whenever an optional column toggles.
func (u *ui) rebuildColumns() {
	prefs := u.app.Preferences()
	showTrack := prefs.BoolWithFallback(prefShowTrackCol, false)
	showFile := prefs.BoolWithFallback(prefShowFilenameCol, false)
	showSelect := prefs.BoolWithFallback(prefShowSelectCol, false)
	showDuration := prefs.BoolWithFallback(prefShowDurationCol, true) // headline of now-playing enrichment
	showFormat := prefs.BoolWithFallback(prefShowFormatCol, false)
	showBitrate := prefs.BoolWithFallback(prefShowBitrateCol, false)
	showGenre := prefs.BoolWithFallback(prefShowGenreCol, false)

	u.visibleCols = u.visibleCols[:0]
	for _, c := range allColumns {
		if c.id == colTrack && !showTrack {
			continue
		}
		if c.id == colFilename && !showFile {
			continue
		}
		if c.id == colSelect && !showSelect {
			continue
		}
		if c.id == colDuration && !showDuration {
			continue
		}
		if c.id == colFormat && !showFormat {
			continue
		}
		if c.id == colBitrate && !showBitrate {
			continue
		}
		if c.id == colGenre && !showGenre {
			continue
		}
		u.visibleCols = append(u.visibleCols, c)
	}
	for i, c := range u.visibleCols {
		u.table.SetColumnWidth(i, c.width)
	}
	u.autosizeFilenameColumn() // widen Filename to fit content (table scrolls if needed)
	u.table.Refresh()
}

// autosizeFilenameColumn widens the Filename column to fit the longest filename
// currently shown (clamped to a sane range), so names aren't truncated. The
// table scrolls horizontally when the window is narrower than the total column
// width; a wide-enough window shows everything without scrolling. No-op when the
// Filename column isn't visible.
func (u *ui) autosizeFilenameColumn() {
	idx := -1
	for i, c := range u.visibleCols {
		if c.id == colFilename {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	const minW, maxW float32 = 160, 640
	size := theme.TextSize()
	widest := fyne.MeasureText("Filename", size, fyne.TextStyle{Bold: true}).Width // at least the header
	for i := range u.tracks {
		if w := fyne.MeasureText(filepath.Base(u.tracks[i].RelPath), size, fyne.TextStyle{}).Width; w > widest {
			widest = w
		}
	}
	w := widest + theme.Padding()*4 // cell insets
	if w < minW {
		w = minW
	} else if w > maxW {
		w = maxW
	}
	u.table.SetColumnWidth(idx, w)
}

// toggleColumn flips an optional column's preference and rebuilds the table and
// menu (so the checkbox state updates).
//
// IMPORTANT (macOS): this runs from a menu item's click while AppKit is still
// tracking that menu. Replacing the main menu inline then crashes with an
// "-[... submenu]: unrecognized selector" NSException. fyne.Do enqueues the work
// on the main loop's func queue, which is drained only after PollEvents (and the
// native menu tracking) returns - so the menu is rebuilt safely after it closes.
func (u *ui) toggleColumn(prefKey string) {
	prefs := u.app.Preferences()
	prefs.SetBool(prefKey, !prefs.BoolWithFallback(prefKey, false))
	fyne.Do(func() {
		u.rebuildColumns()
		u.win.SetMainMenu(buildMenu(u.app, u))
	})
}

// buildTable wires the spreadsheet-style track table with headers.
func (u *ui) buildTable() *widget.Table {
	t := widget.NewTableWithHeaders(
		func() (int, int) { return len(u.tracks), len(u.visibleCols) },
		func() fyne.CanvasObject { return newCellWidget(u) },
		func(id widget.TableCellID, o fyne.CanvasObject) {
			u.updateCell(id, o)
		},
	)
	// Clickable column headers drive sorting (tap to sort, tap again to reverse).
	t.CreateHeader = func() fyne.CanvasObject { return newHeaderWidget(u) }
	t.UpdateHeader = func(id widget.TableCellID, o fyne.CanvasObject) {
		h := o.(*headerWidget)
		h.col = id.Col
		if id.Row == -1 && id.Col >= 0 && id.Col < len(u.visibleCols) {
			c := u.visibleCols[id.Col]
			title := c.title
			// Primary sort: large arrow; secondary (shift-click): small arrow.
			switch {
			case u.sortCol == c.id:
				if u.sortDesc {
					title += " ▼"
				} else {
					title += " ▲"
				}
			case u.sortCol2 == c.id:
				if u.sortDesc2 {
					title += " ▾"
				} else {
					title += " ▴"
				}
			}
			h.label.SetText(title)
		} else {
			h.label.SetText("") // row-number header corner
		}
	}
	t.OnSelected = func(id widget.TableCellID) {
		u.selected = id.Row
		u.refreshStatus()
	}
	return t
}

// updateCell fills one table cell from the backing track slice.
func (u *ui) updateCell(id widget.TableCellID, o fyne.CanvasObject) {
	cell := o.(*cellWidget)
	cell.id = id
	img, lbl := cell.img, cell.label

	if id.Row < 0 || id.Row >= len(u.tracks) || id.Col < 0 || id.Col >= len(u.visibleCols) {
		img.Hide()
		lbl.SetText("")
		return
	}
	tr := u.tracks[id.Row]
	col := u.visibleCols[id.Col]

	if col.id == colArt {
		lbl.Hide()
		if res := u.thumb(tr); res != nil {
			img.Resource = res
			img.Show()
			img.Refresh()
		} else {
			img.Hide()
		}
		return
	}

	img.Hide()
	lbl.Show()
	switch {
	case col.id == colSelect:
		if _, ok := u.marked[tr.ID]; ok {
			lbl.SetText("☑")
		} else {
			lbl.SetText("☐")
		}
	case col.text != nil:
		lbl.SetText(col.text(tr))
	default:
		lbl.SetText("")
	}
}

// cellWidget is one interactive table cell. It stacks an album-art image over
// a text label (only one shows per column) and adds the interactivity a plain
// table lacks: clicking a star directly in the Rating column, and a right-click
// row menu. Non-rating cells fall back to ordinary row selection on tap.
type cellWidget struct {
	widget.BaseWidget
	ui    *ui
	id    widget.TableCellID
	img   *canvas.Image
	label *widget.Label
}

func newCellWidget(u *ui) *cellWidget {
	img := canvas.NewImageFromResource(nil)
	img.FillMode = canvas.ImageFillContain
	img.SetMinSize(fyne.NewSize(44, 44))
	lbl := widget.NewLabel("")
	lbl.Truncation = fyne.TextTruncateEllipsis
	c := &cellWidget{ui: u, img: img, label: lbl}
	c.ExtendBaseWidget(c)
	return c
}

func (c *cellWidget) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewStack(c.img, c.label))
}

// Tapped: in the Rating column, set the rating from which star was clicked
// (clicking the current manual rating again clears it). Elsewhere, select row.
func (c *cellWidget) Tapped(e *fyne.PointEvent) {
	if c.id.Row < 0 || c.id.Row >= len(c.ui.tracks) ||
		c.id.Col < 0 || c.id.Col >= len(c.ui.visibleCols) {
		return
	}
	switch c.ui.visibleCols[c.id.Col].id {
	case colSelect:
		// Handled instantly in MouseDown - see the comment there. Tapped is delayed
		// by the double-tap window (we implement DoubleTapped), so doing it here
		// made each checkbox click feel ~half a second slow.
		return
	case colRating:
		// Map the click to a star by the actual rendered star glyphs, not the full
		// cell width. The stars are a left-aligned text label inset by InnerPadding;
		// dividing the whole cell by 5 (the old way) made the visible 5th star map to
		// 4 and only the empty space past the stars reach 5. Clicks left of / past the
		// stars clamp to 1 / 5, giving generous, accurate hit zones.
		size := theme.TextSize()
		slot := fyne.MeasureText(starString(5), size, fyne.TextStyle{}).Width / 5
		if slot <= 0 {
			return
		}
		star := int((e.Position.X-theme.InnerPadding())/slot) + 1
		if star < 1 {
			star = 1
		} else if star > 5 {
			star = 5
		}
		tr := c.ui.tracks[c.id.Row]
		// Re-clicking an existing manual rating clears it back to auto/unrated.
		if tr.Rating.Valid && int(tr.Rating.Int64) == star {
			star = 0
		}
		c.ui.setRowRating(c.id.Row, star)
		return
	}
	c.ui.table.Select(c.id)
}

func (c *cellWidget) TappedSecondary(e *fyne.PointEvent) {
	c.ui.showRowMenu(c.id.Row, e.AbsolutePosition)
}

// MouseDown toggles the selection checkbox immediately on press. Because the cell
// is DoubleTappable (double-click to play), Fyne delays the normal Tapped callback
// by the double-tap window (~300ms+, the OS double-click speed on macOS) to tell a
// single tap from a double - far too laggy for ticking a box. MouseDown fires at
// once, so the checkbox feels instant; we refresh only that cell, not the table.
func (c *cellWidget) MouseDown(e *desktop.MouseEvent) {
	if e.Button != desktop.MouseButtonPrimary {
		return
	}
	if c.id.Row < 0 || c.id.Row >= len(c.ui.tracks) ||
		c.id.Col < 0 || c.id.Col >= len(c.ui.visibleCols) {
		return
	}
	if c.ui.visibleCols[c.id.Col].id == colSelect {
		c.ui.toggleMark(c.id.Row)
		c.ui.table.RefreshItem(c.id)
	}
}

func (c *cellWidget) MouseUp(*desktop.MouseEvent) {}

// DoubleTapped starts playback from the double-clicked row through the rest of
// the current (filtered/sorted) list. The Rating column is excluded so its
// single-click star setting isn't affected by a double click.
func (c *cellWidget) DoubleTapped(_ *fyne.PointEvent) {
	if c.id.Row < 0 || c.id.Row >= len(c.ui.tracks) ||
		c.id.Col < 0 || c.id.Col >= len(c.ui.visibleCols) {
		return
	}
	if c.ui.visibleCols[c.id.Col].id == colRating {
		return
	}
	c.ui.player.PlayQueue(c.ui.tracks, c.id.Row)
}

// headerWidget is a clickable column header used to drive sorting. Plain click
// sets the primary sort; Shift+click sets a secondary (tie-breaker) sort.
type headerWidget struct {
	widget.BaseWidget
	ui        *ui
	col       int
	label     *widget.Label
	lastShift bool // Shift held at the most recent MouseDown (read by Tapped)
}

func newHeaderWidget(u *ui) *headerWidget {
	l := widget.NewLabel("")
	l.TextStyle = fyne.TextStyle{Bold: true}
	h := &headerWidget{ui: u, col: -2, label: l}
	h.ExtendBaseWidget(h)
	return h
}

func (h *headerWidget) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(h.label)
}

// MouseDown captures the Shift modifier (fyne.PointEvent in Tapped carries no
// modifiers, but desktop.MouseEvent does). MouseDown always precedes Tapped.
func (h *headerWidget) MouseDown(e *desktop.MouseEvent) {
	h.lastShift = e.Modifier&fyne.KeyModifierShift != 0
}

func (h *headerWidget) MouseUp(*desktop.MouseEvent) {}

func (h *headerWidget) Tapped(_ *fyne.PointEvent) {
	if h.col < 0 || h.col >= len(h.ui.visibleCols) {
		return
	}
	id := h.ui.visibleCols[h.col].id // physical -> logical id
	if id == colSelect {
		h.ui.toggleMarkAllShown() // clicking the ✓ header marks/unmarks all shown
		return
	}
	h.ui.sortByColumn(id, h.lastShift)
	h.lastShift = false
}

// sortByColumn applies a sort. A plain click sets the primary sort (toggling its
// direction if already primary, and clearing any secondary). A Shift+click sets
// or toggles the secondary (tie-breaker) sort. The Art column isn't sortable.
func (u *ui) sortByColumn(col int, secondary bool) {
	if col == colArt || col == colSelect {
		return
	}
	if secondary && u.sortCol != -1 && col != u.sortCol {
		if u.sortCol2 == col {
			u.sortDesc2 = !u.sortDesc2
		} else {
			u.sortCol2 = col
			u.sortDesc2 = false
		}
	} else if u.sortCol == col {
		u.sortDesc = !u.sortDesc // re-click primary: flip direction
	} else {
		u.sortCol = col
		u.sortDesc = false
		u.sortCol2 = -1 // a new primary clears the secondary
		u.sortDesc2 = false
	}
	u.reload()
	u.table.Refresh() // redraw headers with the updated sort arrows
	u.saveSortPrefs() // remember the sort sequence for next launch
}

// saveSortPrefs persists the current sort sequence so the table reopens ordered
// the same way next launch (see buildMainWindow's restore).
func (u *ui) saveSortPrefs() {
	prefs := u.app.Preferences()
	prefs.SetInt(prefSortCol, u.sortCol)
	prefs.SetBool(prefSortDesc, u.sortDesc)
	prefs.SetInt(prefSortCol2, u.sortCol2)
	prefs.SetBool(prefSortDesc2, u.sortDesc2)
}

// restoreLastTrack selects and scrolls to the track that was last playing, after
// a short delay so the table has been laid out (the scroll needs a sized
// viewport). It never starts playback. No-op when there's no saved track or it
// isn't in the current view.
func (u *ui) restoreLastTrack() {
	id := int64(u.app.Preferences().Int(prefLastTrackID))
	if id == 0 {
		return
	}
	go func() {
		select {
		case <-time.After(300 * time.Millisecond): // let the window lay out first
		case <-u.tickerDone: // quitting early - don't touch widgets after teardown
			return
		}
		fyne.Do(func() { u.selectAndReveal(id) })
	}()
}

// selectAndReveal selects the row for track id (if shown) and scrolls it cleanly
// into view.
func (u *ui) selectAndReveal(id int64) {
	for i := range u.tracks {
		if u.tracks[i].ID == id {
			u.table.Select(widget.TableCellID{Row: i, Col: 0})
			u.scrollRowIntoView(i)
			return
		}
	}
}

// --- Marking tracks for copy -------------------------------------------------

// markEntry captures what's needed to copy a marked track later: its on-disk
// path plus artist/album for the optional Artist/Album folder layout. Captured
// at mark time so the selection survives filter/sort/reload changes.
type markEntry struct {
	path, artist, album string
}

func markEntryFor(tr Track) markEntry {
	return markEntry{path: tr.AbsPath(), artist: tr.Artist, album: tr.Album}
}

// toggleMark ticks/unticks one row's track in the marked set (by id).
func (u *ui) toggleMark(row int) {
	if row < 0 || row >= len(u.tracks) {
		return
	}
	tr := u.tracks[row]
	if _, ok := u.marked[tr.ID]; ok {
		delete(u.marked, tr.ID)
	} else {
		u.marked[tr.ID] = markEntryFor(tr)
	}
	// The caller refreshes just the toggled cell (RefreshItem); a full table
	// Refresh here would redraw every visible row's album art on each click.
	u.refreshStatus()
}

// toggleMarkAllShown marks every currently-shown track, or unmarks them all if
// they're already all marked (the ✓ header click).
func (u *ui) toggleMarkAllShown() {
	allMarked := len(u.tracks) > 0
	for i := range u.tracks {
		if _, ok := u.marked[u.tracks[i].ID]; !ok {
			allMarked = false
			break
		}
	}
	for i := range u.tracks {
		if allMarked {
			delete(u.marked, u.tracks[i].ID)
		} else {
			u.marked[u.tracks[i].ID] = markEntryFor(u.tracks[i])
		}
	}
	u.table.Refresh()
	u.refreshStatus()
}

// selectAllShown marks all currently-shown tracks (Library menu).
func (u *ui) selectAllShown() {
	for i := range u.tracks {
		u.marked[u.tracks[i].ID] = markEntryFor(u.tracks[i])
	}
	u.table.Refresh()
	u.refreshStatus()
}

// clearSelection unmarks everything (Library menu).
func (u *ui) clearSelection() {
	u.marked = map[int64]markEntry{}
	u.table.Refresh()
	u.refreshStatus()
}

const (
	copyLayoutFlat      = "Flat (all files in one folder)"
	copyLayoutOrganized = "Organize into Artist/Album folders"
)

// copySelectedTo asks for a layout (flat vs Artist/Album) then a destination
// folder, and copies the marked tracks' files there.
func (u *ui) copySelectedTo() {
	if len(u.marked) == 0 {
		dialog.ShowInformation("Copy selected",
			"No tracks are marked. Turn on View → Selection checkboxes and tick some "+
				"tracks (or Library → Select all shown), then try again.", u.win)
		return
	}
	entries := make([]markEntry, 0, len(u.marked))
	for _, e := range u.marked {
		entries = append(entries, e)
	}

	layout := widget.NewRadioGroup([]string{copyLayoutFlat, copyLayoutOrganized}, nil)
	layout.SetSelected(copyLayoutFlat)
	body := container.NewVBox(
		widget.NewLabel(fmt.Sprintf("Copy %d track(s). Choose a layout, then a destination folder.", len(entries))),
		layout,
	)
	dialog.ShowCustomConfirm("Copy selected", "Choose folder…", "Cancel", body, func(ok bool) {
		if !ok {
			return
		}
		organize := layout.Selected == copyLayoutOrganized
		dialog.ShowFolderOpen(func(list fyne.ListableURI, err error) {
			if err != nil || list == nil {
				return
			}
			u.copyEntriesTo(entries, list.Path(), organize)
		}, u.win)
	}, u.win)
}

// copyEntriesTo copies the marked files into destDir on a background goroutine.
// When organize is set, each file goes under <dest>/<Artist>/<Album>/. A modal
// dialog shows a progress bar and the current filename, with a Cancel button
// that stops before the next file. Filename collisions get a " (n)" suffix.
func (u *ui) copyEntriesTo(entries []markEntry, destDir string, organize bool) {
	total := len(entries)

	statusLbl := widget.NewLabel(fmt.Sprintf("Copying %d file(s)…", total))
	statusLbl.Truncation = fyne.TextTruncateEllipsis
	prog := widget.NewProgressBar()
	prog.Max = float64(total)
	var canceled atomic.Bool
	cancelBtn := widget.NewButton("Cancel", func() { canceled.Store(true) })
	cancelBtn.Importance = widget.DangerImportance
	d := dialog.NewCustomWithoutButtons("Copying files", container.NewVBox(
		statusLbl, prog, container.NewCenter(cancelBtn),
	), u.win)
	d.Resize(fyne.NewSize(420, 150))
	d.Show()

	go func() {
		var copied, failed int
		for i, e := range entries {
			if canceled.Load() {
				break
			}
			n, name := i+1, filepath.Base(e.path)
			fyne.Do(func() { statusLbl.SetText(fmt.Sprintf("Copying %d of %d: %s", n, total, name)) })
			target := destDir
			if organize {
				target = filepath.Join(destDir,
					sanitizeFolderName(e.artist, "Unknown Artist"),
					sanitizeFolderName(e.album, "Unknown Album"))
				if err := os.MkdirAll(target, 0o755); err != nil {
					log.Printf("copy: mkdir %q: %v", target, err)
					failed++
					fyne.Do(func() { prog.SetValue(float64(n)) })
					continue
				}
			}
			if err := copyFileInto(e.path, target); err != nil {
				log.Printf("copy %q -> %q: %v", e.path, target, err)
				failed++
			} else {
				copied++
			}
			fyne.Do(func() { prog.SetValue(float64(n)) })
		}
		wasCanceled := canceled.Load()
		fyne.Do(func() {
			d.Hide()
			summary := fmt.Sprintf("Copied %d file(s) to %s", copied, destDir)
			if failed > 0 {
				summary += fmt.Sprintf("; %d failed (see log)", failed)
			}
			title := "Copy complete"
			if wasCanceled {
				title = "Copy cancelled"
				summary = "Cancelled before finishing. " + summary
			}
			u.status.SetText(summary)
			dialog.ShowInformation(title, summary, u.win)
		})
	}()
}

// enqueueRow appends a single library row to the play queue.
func (u *ui) enqueueRow(row int) {
	if row < 0 || row >= len(u.tracks) {
		return
	}
	u.player.Enqueue([]Track{u.tracks[row]})
	u.status.SetText("Added to queue: " + queueRowText(u.tracks[row]))
}

// enqueueShown appends every track in the current (filtered) view to the queue.
func (u *ui) enqueueShown() {
	if len(u.tracks) == 0 {
		dialog.ShowInformation("Add to queue", "No tracks are shown.", u.win)
		return
	}
	u.player.Enqueue(u.tracks)
	u.status.SetText(fmt.Sprintf("Added %d track(s) to the queue", len(u.tracks)))
}

// enqueueSelected appends the marked tracks that are in the current view to the
// queue (the filter → Select All Shown → Add to Queue workflow). Marked tracks
// hidden by the current search/filter are reported but skipped.
func (u *ui) enqueueSelected() {
	if len(u.marked) == 0 {
		dialog.ShowInformation("Add to queue",
			"No tracks are marked. Turn on View → Selection checkboxes and tick some "+
				"tracks (or Library → Select All Shown), then try again.", u.win)
		return
	}
	var sel []Track
	for i := range u.tracks {
		if _, ok := u.marked[u.tracks[i].ID]; ok {
			sel = append(sel, u.tracks[i])
		}
	}
	if len(sel) == 0 {
		dialog.ShowInformation("Add to queue",
			"None of the marked tracks are in the current view. Clear the search/filter and try again.", u.win)
		return
	}
	u.player.Enqueue(sel)
	msg := fmt.Sprintf("Added %d marked track(s) to the queue", len(sel))
	if miss := len(u.marked) - len(sel); miss > 0 {
		msg += fmt.Sprintf(" (%d not in current view)", miss)
	}
	u.status.SetText(msg)
}

// sanitizeFolderName makes s safe as a single folder name (replacing characters
// illegal on common filesystems), falling back when empty.
func sanitizeFolderName(s, fallback string) string {
	s = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '_'
		}
		if r < 0x20 {
			return '_'
		}
		return r
	}, strings.TrimSpace(s))
	s = strings.Trim(s, " .") // trailing dots/spaces are problematic on Windows
	if s == "" {
		return fallback
	}
	return s
}

// copyFileInto copies src into destDir using src's base name, avoiding overwrite
// by appending " (n)" before the extension on collision.
func copyFileInto(src, destDir string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(uniqueDestPath(destDir, filepath.Base(src)))
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// uniqueDestPath returns dir/name, or dir/name (n).ext if that already exists.
func uniqueDestPath(dir, name string) string {
	target := filepath.Join(dir, name)
	if !checkFileExists(target) {
		return target
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for n := 2; ; n++ {
		cand := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, n, ext))
		if !checkFileExists(cand) {
			return cand
		}
	}
}

// setRowRating sets (1..5) or clears (0) the manual rating of a row. It updates
// the row in place (no reload) so rating a track doesn't reset the scroll/sort or
// jump the list - mirroring onPlayCounted. A row whose new rating no longer
// matches an active rating filter stays until the next reload, which is fine.
func (u *ui) setRowRating(row, rating int) {
	if row < 0 || row >= len(u.tracks) {
		return
	}
	if err := u.db.SetRating(u.tracks[row].ID, rating); err != nil {
		dialog.ShowError(err, u.win)
		return
	}
	if rating <= 0 {
		u.tracks[row].Rating = sql.NullInt64{} // cleared -> falls back to auto rating
	} else {
		u.tracks[row].Rating = sql.NullInt64{Int64: int64(rating), Valid: true}
	}
	u.table.Refresh()
}

// setMarkedRating applies a rating (0 clears) to every marked track at once, updating
// the catalog and the in-memory rows, then refreshing once. Mirrors setRowRating for
// the right-click-on-a-marked-row case.
func (u *ui) setMarkedRating(rating int) {
	var nr sql.NullInt64
	if rating > 0 {
		nr = sql.NullInt64{Int64: int64(rating), Valid: true}
	}
	for id := range u.marked {
		if err := u.db.SetRating(id, rating); err != nil {
			dialog.ShowError(err, u.win)
			return
		}
		for i := range u.tracks {
			if u.tracks[i].ID == id {
				u.tracks[i].Rating = nr
				break
			}
		}
	}
	u.table.Refresh()
	u.status.SetText(fmt.Sprintf("Rated %d marked track(s)", len(u.marked)))
}

// showRowMenu pops up the right-click row menu. Play and rating are live. Edit
// tags / Rename follow the usual selection convention: right-clicking a marked row
// acts on the whole marked set (batch), while right-clicking an unmarked row acts on
// just that row - the menu label says which.
func (u *ui) showRowMenu(row int, pos fyne.Position) {
	if row < 0 || row >= len(u.tracks) {
		return
	}
	u.selected = row
	u.refreshStatus()
	r := row

	// Edit tags / Rename / Set rating act on the marked set when the right-clicked row
	// is itself marked, otherwise on just this row (Explorer/foobar convention).
	_, rowMarked := u.marked[u.tracks[r].ID]
	nMarked := len(u.marked)

	// setRating routes a chosen rating to the marked set or the single row.
	setRating := func(n int) {
		if rowMarked {
			u.setMarkedRating(n)
		} else {
			u.setRowRating(r, n)
		}
	}
	star := func(n int) *fyne.MenuItem {
		return fyne.NewMenuItem(fmt.Sprintf("%s  (%d)", starString(n), n),
			func() { setRating(n) })
	}
	ratingLabel := "Set rating"
	if rowMarked {
		ratingLabel = fmt.Sprintf("Set rating (%d marked)", nMarked)
	}
	ratingItem := fyne.NewMenuItem(ratingLabel, nil)
	ratingItem.ChildMenu = fyne.NewMenu("",
		star(5), star(4), star(3), star(2), star(1),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Clear rating", func() { setRating(0) }),
	)

	// Album art is stored in the catalog (shown everywhere), not embedded into
	// the audio file. Source: a local image or an explicit URL - no online search.
	artItem := fyne.NewMenuItem("Add album art", nil)
	artItem.ChildMenu = fyne.NewMenu("",
		fyne.NewMenuItem("From image file…", func() { u.addArtFromFile(r) }),
		fyne.NewMenuItem("From URL…", func() { u.addArtFromURL(r) }),
	)

	renameLabel, editLabel := "Rename file…", "Edit tags…"
	renameFn := func() { u.renameFromTags(r) }
	editFn := func() { u.editTags(r) }
	if rowMarked {
		renameLabel = fmt.Sprintf("Rename %d marked from pattern…", nMarked)
		editLabel = fmt.Sprintf("Edit tags of %d marked…", nMarked)
		renameFn = u.renameSelectedFromTags
		editFn = u.editTagsOfSelected
	}

	menu := fyne.NewMenu("",
		fyne.NewMenuItem("Play", func() { u.player.PlayQueue(u.tracks, r) }),
		fyne.NewMenuItem("Add to Queue", func() { u.enqueueRow(r) }),
		ratingItem,
		artItem,
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Show in "+fileManagerName(), func() { u.revealRow(r) }),
		fyne.NewMenuItem("Show full path…", func() { u.showFullPath(r) }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem(renameLabel, renameFn),
		fyne.NewMenuItem(editLabel, editFn),
	)
	widget.ShowPopUpMenuAtPosition(menu, u.win.Canvas(), pos)
}

// fileManagerName is the OS file manager's name, for the right-click label.
func fileManagerName() string {
	switch runtime.GOOS {
	case "darwin":
		return "Finder"
	case "windows":
		return "Explorer"
	default:
		return "File Manager"
	}
}

// revealInFileManager opens the OS file manager showing the given file. On macOS
// and Windows the file is selected/highlighted; on Linux (no portable "reveal")
// the containing folder is opened. Uses Start (fire-and-forget): the launchers
// don't exit cleanly, and Windows' explorer returns a non-zero code on success.
func revealInFileManager(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", "-R", path).Start()
	case "windows":
		return exec.Command("explorer", "/select,"+path).Start()
	default:
		return exec.Command("xdg-open", filepath.Dir(path)).Start()
	}
}

// revealRow shows the row's file in the OS file manager.
func (u *ui) revealRow(row int) {
	if row < 0 || row >= len(u.tracks) {
		return
	}
	if err := revealInFileManager(u.tracks[row].AbsPath()); err != nil {
		dialog.ShowError(fmt.Errorf("could not open %s: %w", fileManagerName(), err), u.win)
	}
}

// showFullPath shows the row's absolute path in a dialog, selectable and with a
// Copy button (the catalog stores paths relative to the watched folder, so this
// reconstructs the on-disk location).
func (u *ui) showFullPath(row int) {
	if row < 0 || row >= len(u.tracks) {
		return
	}
	path := u.tracks[row].AbsPath()
	box := widget.NewMultiLineEntry()
	box.SetText(path)
	box.Wrapping = fyne.TextWrapBreak
	copyBtn := widget.NewButtonWithIcon("Copy", theme.ContentCopyIcon(), func() {
		u.win.Clipboard().SetContent(path)
	})
	content := container.NewBorder(nil, copyBtn, nil, nil, box)
	d := dialog.NewCustom("Full path", "Close", content, u.win)
	d.Resize(fyne.NewSize(560, 200))
	d.Show()
}

// thumb returns (and caches) the album-art thumbnail resource for a track, or
// nil if the track has none. It always consults the DB on a cache miss (no
// HasArt short-circuit) so art added at runtime shows up after the cache entry
// is invalidated. Misses are cached as nil, so each track is queried at most
// once per view.
func (u *ui) thumb(tr Track) fyne.Resource {
	if res, ok := u.thumbCache[tr.ID]; ok {
		return res
	}
	png := u.db.Thumb(tr.ID)
	if png == nil {
		u.thumbCache[tr.ID] = nil
		return nil
	}
	res := fyne.NewStaticResource(fmt.Sprintf("art-%d.png", tr.ID), png)
	u.thumbCache[tr.ID] = res
	return res
}

// addArtFromFile lets the user pick a local image and sets it as the track's art.
func (u *ui) addArtFromFile(row int) {
	if row < 0 || row >= len(u.tracks) {
		return
	}
	id := u.tracks[row].ID
	fd := dialog.NewFileOpen(func(rc fyne.URIReadCloser, err error) {
		if err != nil || rc == nil {
			return
		}
		defer rc.Close()
		data, err := io.ReadAll(io.LimitReader(rc, maxArtBytes))
		if err != nil {
			dialog.ShowError(err, u.win)
			return
		}
		u.applyArt(id, data)
	}, u.win)
	fd.SetFilter(storage.NewExtensionFileFilter([]string{".png", ".jpg", ".jpeg", ".gif"}))
	fd.Show()
}

// addArtFromURL fetches an image from a user-supplied URL and sets it as the art.
func (u *ui) addArtFromURL(row int) {
	if row < 0 || row >= len(u.tracks) {
		return
	}
	id := u.tracks[row].ID
	entry := widget.NewEntry()
	entry.SetPlaceHolder("https://example.com/cover.jpg")
	d := dialog.NewForm("Add album art from URL", "Fetch", "Cancel",
		[]*widget.FormItem{widget.NewFormItem("Image URL", entry)},
		func(ok bool) {
			url := strings.TrimSpace(entry.Text)
			if !ok || url == "" {
				return
			}
			go func() { // network fetch off the UI thread
				data, err := fetchImage(url)
				fyne.Do(func() {
					if err != nil {
						dialog.ShowError(err, u.win)
						return
					}
					u.applyArt(id, data)
				})
			}()
		}, u.win)
	d.Resize(fyne.NewSize(560, 160)) // wide enough to see/type a full URL
	d.Show()
}

// applyArt thumbnails the image bytes, stores them as the track's art, and
// refreshes the table + now-playing so the new art shows immediately.
func (u *ui) applyArt(trackID int64, data []byte) {
	png, err := makeThumbnail(data, thumbSize)
	if err != nil {
		dialog.ShowError(fmt.Errorf("could not read image: %w", err), u.win)
		return
	}
	if err := u.db.SetArt(trackID, png); err != nil {
		dialog.ShowError(err, u.win)
		return
	}
	delete(u.thumbCache, trackID) // force re-read of the new art
	for i := range u.tracks {
		if u.tracks[i].ID == trackID {
			u.tracks[i].HasArt = true
			break
		}
	}
	u.table.Refresh()
	u.refreshNowPlaying()
	u.status.SetText("Album art updated")
}

// fetchImage downloads image bytes from url with a timeout and a size cap.
func fetchImage(url string) ([]byte, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxArtBytes))
}

// reload re-queries the catalog with the current filter and refreshes the table.
func (u *ui) reload() {
	tracks, err := u.db.Tracks(TrackQuery{
		Filter:    u.filter,
		Search:    u.search,
		Genre:     u.qGenre,
		Artist:    u.qArtist,
		Album:     u.qAlbum,
		FTitle:    u.fTitle,
		FArtist:   u.fArtist,
		FAlbum:    u.fAlbum,
		FGenre:    u.fGenre,
		FYear:     u.fYear,
		FPlays:    u.fPlays,
		SortCol:   u.sortCol,
		Desc:      u.sortDesc,
		Sort2Col:  u.sortCol2,
		Sort2Desc: u.sortDesc2,
	})
	if err != nil {
		dialog.ShowError(err, u.win)
		return
	}
	u.tracks = tracks
	u.selected = -1
	u.thumbCache = map[int64]fyne.Resource{}
	u.autosizeFilenameColumn() // fit the Filename column to the loaded data
	u.table.Refresh()
	for r := range u.tracks {
		u.table.SetRowHeight(r, tableRowHeight)
	}
	// Reset the vertical scroll to the top. reload() means the result set changed
	// (filter/search/sort/playlist/scan), so the top is the right place to land -
	// and, crucially, it forces a relayout from a valid offset. Without this, when
	// the list shrinks (e.g. 177 -> 10) a previously-scrolled offset can sit past
	// the new content: Fyne's row-height path doesn't clamp it, so the table shows
	// blank/stale rows until the user scrolls. Horizontal position is preserved.
	u.table.ScrollToTop()
	u.refreshStatus()
}

func (u *ui) refreshStatus() {
	s := fmt.Sprintf("%d tracks", len(u.tracks))
	if u.smartName != "" {
		s = "♫ " + u.smartName + "  |  " + s
	}
	if len(u.marked) > 0 {
		s += fmt.Sprintf("  |  %d marked for copy", len(u.marked))
	}
	if u.selected >= 0 && u.selected < len(u.tracks) {
		s += "  |  selected: " + u.tracks[u.selected].Title
	}
	u.status.SetText(s)
}

// refreshNowPlaying syncs the transport bar with the player. Runs on UI thread.
func (u *ui) refreshNowPlaying() {
	u.refreshQueue() // keep the Play Queue window in sync when it's open
	tr, ok := u.player.Current()
	if !ok {
		u.nowPlaying.SetText("Nothing playing")
		u.nowArt.Resource = resourceKrankyBearMediaPlayerPng
		u.nowArt.Refresh()
		u.playPause.SetIcon(theme.MediaPlayIcon())
		u.nowPlayingID = 0 // next track that plays will move the indicator
		return
	}
	u.nowPlaying.SetText(fmt.Sprintf("%s — %s", tr.Title, tr.Artist))
	if res := u.thumb(tr); res != nil {
		u.nowArt.Resource = res
	} else {
		u.nowArt.Resource = resourceKrankyBearMediaPlayerPng
	}
	u.nowArt.Refresh()
	if u.player.IsPlaying() {
		u.playPause.SetIcon(theme.MediaPauseIcon())
	} else {
		u.playPause.SetIcon(theme.MediaPlayIcon())
	}
	// When the playing track changes, move the selection indicator to follow it
	// (only on change, so we don't override a manual selection mid-track).
	if tr.ID != u.nowPlayingID {
		u.nowPlayingID = tr.ID
		u.selectTrack(tr.ID)
		u.app.Preferences().SetInt(prefLastTrackID, int(tr.ID)) // restore this track on next launch
	}
	// Play counts may have changed; reflect them in the table.
	u.table.Refresh()
}

// selectTrack highlights and scrolls to the row for the given track id, if it's
// in the current (filtered/sorted) view. No-op if the track isn't shown.
func (u *ui) selectTrack(id int64) {
	for i := range u.tracks {
		if u.tracks[i].ID == id {
			cell := widget.TableCellID{Row: i, Col: 0}
			u.table.Select(cell) // also updates u.selected via OnSelected
			u.table.ScrollTo(cell)
			return
		}
	}
}

// jumpToCurrent scrolls to and selects the now-playing track. It reports via the
// status bar when nothing is playing or the track is hidden by the current
// filter/search.
func (u *ui) jumpToCurrent() {
	cur, ok := u.player.Current()
	if !ok {
		u.status.SetText("Nothing is playing")
		return
	}
	for i := range u.tracks {
		if u.tracks[i].ID == cur.ID {
			u.table.Select(widget.TableCellID{Row: i, Col: 0})
			u.scrollRowIntoView(i)
			return
		}
	}
	u.status.SetText("The playing track isn't in the current view")
}

// scrollRowIntoView positions the table so row i sits about one row below the
// sticky header, rather than flush against an edge where the header (top) or the
// horizontal scrollbar (bottom) would clip it - which is what Table.ScrollTo
// does. Rows are laid out as tableRowHeight plus one padding each (see Fyne's
// Table.findY); the table fixes every row to tableRowHeight, so the offset is
// exact. Horizontal offset resets to the leading edge, showing the main columns.
func (u *ui) scrollRowIntoView(i int) {
	pad := theme.Padding()
	margin := tableRowHeight + pad // keep ~one row of context above the target
	off := float32(i)*(tableRowHeight+pad) - margin
	if off < 0 {
		off = 0
	}
	u.table.ScrollToOffset(fyne.NewPos(0, off))
}

// onPlayPause starts the selected track if nothing is loaded, else pauses.
func (u *ui) onPlayPause() {
	// A live stream (playing or paused) → toggle. After Stop there's no stream
	// even though a track stays "current", so fall through to (re)start it.
	if u.player.HasStream() {
		u.player.TogglePause()
		return
	}
	if u.player.ResumeCurrent() { // replay the stopped track if the queue is intact
		return
	}
	start := u.selected
	if start < 0 {
		start = 0
	}
	if len(u.tracks) == 0 {
		return
	}
	u.player.PlayQueue(u.tracks, start)
}

// rateSelected sets (1..5) or clears (0) the manual rating of the selected row.
func (u *ui) rateSelected(rating int) {
	if u.selected < 0 || u.selected >= len(u.tracks) {
		dialog.ShowInformation("No selection", "Select a track first.", u.win)
		return
	}
	// Update in place (same as the in-row stars) - not via reload(), which rebuilds
	// the list and momentarily blanks it until the next scroll (a Fyne table quirk).
	u.setRowRating(u.selected, rating)
}

// addFolder lets the user pick a folder, catalogs it, and refreshes.
func (u *ui) addFolder() {
	dialog.ShowFolderOpen(func(list fyne.ListableURI, err error) {
		if err != nil || list == nil {
			return
		}
		path := list.Path()
		id, err := u.db.AddFolder(path, true)
		if err != nil {
			dialog.ShowError(err, u.win)
			return
		}
		u.scanFolders([]Folder{{ID: id, Path: path, Recursive: true}})
	}, u.win)
}

// relocateFolder lets the user repoint a watched folder's root at a new path -
// the fix for a portable drive that mounts as D:\ on one machine and E:\ on
// another. Because tracks are stored relative to the root, repointing it
// instantly relocates every track under it (no per-file fixups, no re-scan).
func (u *ui) relocateFolder() {
	folders, err := u.db.Folders()
	if err != nil {
		dialog.ShowError(err, u.win)
		return
	}
	if len(folders) == 0 {
		dialog.ShowInformation("No folders", "Add a folder first.", u.win)
		return
	}
	names := make([]string, len(folders))
	for i, f := range folders {
		names[i] = f.Path
	}
	sel := widget.NewSelect(names, nil)
	sel.SetSelectedIndex(0)
	body := container.NewVBox(widget.NewLabel("Folder to relocate:"), sel)
	dialog.ShowCustomConfirm("Relocate folder", "Choose new root…", "Cancel", body,
		func(ok bool) {
			if !ok || sel.SelectedIndex() < 0 {
				return
			}
			folder := folders[sel.SelectedIndex()]
			dialog.ShowFolderOpen(func(list fyne.ListableURI, err error) {
				if err != nil || list == nil {
					return
				}
				if err := u.db.RelocateFolder(folder.ID, list.Path()); err != nil {
					dialog.ShowError(err, u.win)
					return
				}
				u.reload()
				u.status.SetText("Relocated to: " + list.Path())
			}, u.win)
		}, u.win)
}

// rescanAll re-scans every watched folder (picking up new/changed files).
func (u *ui) rescanAll() {
	folders, err := u.db.Folders()
	if err != nil {
		dialog.ShowError(err, u.win)
		return
	}
	if len(folders) == 0 {
		dialog.ShowInformation("No folders", "Add a folder first.", u.win)
		return
	}
	u.scanFolders(folders)
}

// scanFolders scans the given folders on a background goroutine, updating the
// status line and reloading the table when done. Any onDone callbacks run on the
// UI goroutine after the reload (e.g. the folder manager refreshing its list).
func (u *ui) scanFolders(folders []Folder, onDone ...func()) {
	go func() {
		var total ScanResult
		for _, f := range folders {
			res, err := u.db.ScanFolder(f, func(p string) {
				fyne.Do(func() { u.status.SetText("Scanning: " + p) })
			})
			if err != nil {
				log.Printf("scan %q: %v", f.Path, err)
			}
			total.Found += res.Found
			total.Updated += res.Updated
			total.Errors += res.Errors
		}
		fyne.Do(func() {
			u.reload()
			u.status.SetText(fmt.Sprintf("Scan complete: %d files, %d catalogued, %d errors",
				total.Found, total.Updated, total.Errors))
			u.startDurationEnricher() // compute lengths for any newly catalogued files
			for _, fn := range onDone {
				fn()
			}
		})
	}()
}
