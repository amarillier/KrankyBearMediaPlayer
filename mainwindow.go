// Package main - mainwindow.go builds the library window: a spreadsheet-style
// table of catalogued tracks (with embedded album-art thumbnails), a rating
// filter, folder management, and the playback transport.
package main

import (
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
	colFilename
	colSelect // checkbox column for marking tracks (copy to media)
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
	{colYear, "Year", 56, false, func(tr Track) string {
		if tr.Year > 0 {
			return fmt.Sprintf("%d", tr.Year)
		}
		return ""
	}},
	{colPlays, "Plays", 64, false, func(tr Track) string { return fmt.Sprintf("%d", tr.PlayCount) }},
	{colRating, "Rating", 96, false, func(tr Track) string { return starString(tr.EffectiveRating()) }},
	{colFilename, "Filename", 220, true, func(tr Track) string { return filepath.Base(tr.RelPath) }},
}

// Preference keys for the optional columns (remembered across launches).
const (
	prefShowTrackCol    = "showTrackColumn"
	prefShowFilenameCol = "showFilenameColumn"
	prefShowSelectCol   = "showSelectColumn"
)

// prefPlayCountPct is the percentage of a track that must play before it counts
// as a play (and bumps the auto rating). 100 = only at natural end.
const prefPlayCountPct = "playCountPercent"
const defaultPlayCountPct = 50

// Playback mode preferences (remembered across launches).
const prefShuffle = "shuffle"   // bool
const prefRepeat = "repeatMode" // int: 0 off, 1 all, 2 one
const prefVolume = "volume"     // float 0..1 linear gain

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

	tracks       []Track
	filter       Filter
	search       string
	qGenre       string // active smart-playlist constraints (empty = none)
	qArtist      string
	qAlbum       string
	smartName    string // name of the applied smart playlist, "" if none
	sortCol      int    // primary sort column, -1 = default order
	sortDesc     bool   // primary descending
	sortCol2     int    // secondary sort column (shift-click), -1 = none
	sortDesc2    bool   // secondary descending
	selected     int    // selected table row, -1 if none
	nowPlayingID int64  // track id last followed by the selection indicator

	marked map[int64]markEntry // tracks ticked for copy: id -> path+artist+album

	table       *widget.Table
	visibleCols []columnDef // currently shown columns, in physical order
	thumbCache  map[int64]fyne.Resource

	nowPlaying *widget.Label
	nowArt     *canvas.Image
	playPause  *widget.Button
	status     *widget.Label

	seekSlider *widget.Slider
	timeLabel  *widget.Label
	seeking    bool // true while the user drags the seek slider
	volSlider  *widget.Slider

	// Play Queue window state (see queue.go).
	queueList    *widget.List
	queueTracks  []Track // queued tracks in play order (snapshot for the list)
	queueCurrent int     // play-order position of the now-playing track, -1 if none
	queueSel     int     // selected row in the queue list, -1 if none

	tickerDone chan struct{} // closed to stop the progress ticker (on quit)
	tickerOnce sync.Once     // guards closing tickerDone exactly once
}

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
	bottom := u.buildTransport()

	content := container.NewBorder(top, bottom, nil, nil, u.table)
	win.SetContent(content)

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
	vol := prefs.FloatWithFallback(prefVolume, 1.0)
	player.SetVolume(vol)
	u.volSlider.SetValue(vol) // reflect the restored volume in the slider

	u.reload()
	u.startProgressTicker()
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
	addBtn := widget.NewButtonWithIcon("Add Folder", theme.FolderOpenIcon(), u.addFolder)
	rescanBtn := widget.NewButtonWithIcon("Rescan", theme.ViewRefreshIcon(), u.rescanAll)

	filterSel := widget.NewSelect(filterLabels(), nil)
	// Set the default selection before wiring the callback so we don't trigger
	// a reload mid-construction (other widgets aren't built yet).
	filterSel.SetSelectedIndex(0)
	filterSel.OnChanged = func(label string) {
		u.filter = filterByLabel(label)
		u.clearSmartCriteria() // a manual filter change leaves any smart playlist
		u.reload()
	}

	left := container.NewHBox(addBtn, rescanBtn, widget.NewSeparator(),
		widget.NewLabel("Show:"), filterSel)

	// Search across title/artist/album/genre/filename (handled in SQL).
	search := widget.NewEntry()
	search.SetPlaceHolder("Search title, artist, album, genre, filename…")
	search.OnChanged = func(s string) {
		u.search = s
		u.clearSmartCriteria() // typing a search leaves any smart playlist
		u.reload()
	}
	clearSearch := widget.NewButtonWithIcon("", theme.ContentClearIcon(), func() {
		search.SetText("") // fires OnChanged -> reload
	})

	// Search box expands to fill the width between the controls and the button.
	searchRow := container.NewBorder(nil, nil, left, clearSearch, search)
	return container.NewVBox(searchRow, widget.NewSeparator())
}

// buildTransport builds the bottom bar: now-playing, transport, rating setter.
func (u *ui) buildTransport() fyne.CanvasObject {
	u.nowArt = canvas.NewImageFromResource(resourceKrankyBearMediaPlayerPng)
	u.nowArt.FillMode = canvas.ImageFillContain
	u.nowArt.SetMinSize(fyne.NewSize(40, 40))

	u.nowPlaying = widget.NewLabel("Nothing playing")
	u.nowPlaying.Wrapping = fyne.TextTruncate

	prev := widget.NewButtonWithIcon("", theme.MediaSkipPreviousIcon(), u.player.Prev)
	u.playPause = widget.NewButtonWithIcon("", theme.MediaPlayIcon(), u.onPlayPause)
	stop := widget.NewButtonWithIcon("", theme.MediaStopIcon(), u.player.Stop)
	next := widget.NewButtonWithIcon("", theme.MediaSkipNextIcon(), u.player.Next)
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

	// Volume slider (0..1 linear gain), fixed width.
	u.volSlider = widget.NewSlider(0, 1)
	u.volSlider.Step = 0.01
	u.volSlider.Value = u.player.Volume()
	u.volSlider.OnChanged = func(v float64) {
		u.player.SetVolume(v)
		u.app.Preferences().SetFloat(prefVolume, v) // remember across launches
	}
	volBox := container.NewBorder(nil, nil, widget.NewIcon(theme.VolumeUpIcon()), nil,
		container.NewGridWrap(fyne.NewSize(110, 28), u.volSlider))

	right := container.NewHBox(volBox, widget.NewSeparator(), u.buildRatingBar())
	nowBox := container.NewBorder(nil, nil, u.nowArt, nil, u.nowPlaying)
	controls := container.NewBorder(nil, nil, transport, right, nowBox)

	return container.NewVBox(widget.NewSeparator(), seekRow, controls, u.status)
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
	showTrack := u.app.Preferences().BoolWithFallback(prefShowTrackCol, false)
	showFile := u.app.Preferences().BoolWithFallback(prefShowFilenameCol, false)
	showSelect := u.app.Preferences().BoolWithFallback(prefShowSelectCol, false)

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
		w := c.Size().Width
		if w <= 0 {
			return
		}
		star := int(e.Position.X/(w/5)) + 1
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

// setRowRating sets (1..5) or clears (0) the manual rating of a row, then reloads.
func (u *ui) setRowRating(row, rating int) {
	if row < 0 || row >= len(u.tracks) {
		return
	}
	if err := u.db.SetRating(u.tracks[row].ID, rating); err != nil {
		dialog.ShowError(err, u.win)
		return
	}
	u.reload()
}

// showRowMenu pops up the right-click row menu. Play and rating are live; the
// file-management actions are intentionally stubbed as "coming soon" so the
// menu advertises the roadmap (rename / tag edit / album-art add).
func (u *ui) showRowMenu(row int, pos fyne.Position) {
	if row < 0 || row >= len(u.tracks) {
		return
	}
	u.selected = row
	u.refreshStatus()
	r := row

	star := func(n int) *fyne.MenuItem {
		return fyne.NewMenuItem(fmt.Sprintf("%s  (%d)", starString(n), n),
			func() { u.setRowRating(r, n) })
	}
	ratingItem := fyne.NewMenuItem("Set rating", nil)
	ratingItem.ChildMenu = fyne.NewMenu("",
		star(5), star(4), star(3), star(2), star(1),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Clear rating", func() { u.setRowRating(r, 0) }),
	)

	soon := func(label string) *fyne.MenuItem {
		it := fyne.NewMenuItem(label, nil)
		it.Disabled = true
		return it
	}

	// Album art is stored in the catalog (shown everywhere), not embedded into
	// the audio file. Source: a local image or an explicit URL - no online search.
	artItem := fyne.NewMenuItem("Add album art", nil)
	artItem.ChildMenu = fyne.NewMenu("",
		fyne.NewMenuItem("From image file…", func() { u.addArtFromFile(r) }),
		fyne.NewMenuItem("From URL…", func() { u.addArtFromURL(r) }),
	)

	menu := fyne.NewMenu("",
		fyne.NewMenuItem("Play", func() { u.player.PlayQueue(u.tracks, r) }),
		fyne.NewMenuItem("Add to Queue", func() { u.enqueueRow(r) }),
		ratingItem,
		artItem,
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Show in "+fileManagerName(), func() { u.revealRow(r) }),
		fyne.NewMenuItem("Show full path…", func() { u.showFullPath(r) }),
		fyne.NewMenuItemSeparator(),
		soon("Rename file…  (coming soon)"),
		soon("Edit tags…  (coming soon)"),
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

// onPlayPause starts the selected track if nothing is loaded, else pauses.
func (u *ui) onPlayPause() {
	if _, ok := u.player.Current(); ok {
		u.player.TogglePause()
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
	tr := u.tracks[u.selected]
	if err := u.db.SetRating(tr.ID, rating); err != nil {
		dialog.ShowError(err, u.win)
		return
	}
	u.reload()
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
// status line and reloading the table when done.
func (u *ui) scanFolders(folders []Folder) {
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
		})
	}()
}
