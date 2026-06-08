// Package main - mainwindow.go builds the library window: a spreadsheet-style
// table of catalogued tracks (with embedded album-art thumbnails), a rating
// filter, folder management, and the playback transport.
package main

import (
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
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
)

// prefPlayCountPct is the percentage of a track that must play before it counts
// as a play (and bumps the auto rating). 100 = only at natural end.
const prefPlayCountPct = "playCountPercent"
const defaultPlayCountPct = 50

const tableRowHeight = 52

// ui holds the live widgets and state of the main window.
type ui struct {
	app    fyne.App
	win    fyne.Window
	db     *DB
	player *Player

	tracks       []Track
	filter       Filter
	search       string
	sortCol      int   // active sort column, -1 = default order
	sortDesc     bool  // descending sort
	selected     int   // selected table row, -1 if none
	nowPlayingID int64 // track id last followed by the selection indicator

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

	tickerDone chan struct{} // closed to stop the progress ticker (on quit)
	tickerOnce sync.Once     // guards closing tickerDone exactly once
}

// buildMainWindow constructs and populates the main window content.
func buildMainWindow(a fyne.App, win fyne.Window, db *DB, player *Player) *ui {
	u := &ui{
		app:        a,
		win:        win,
		db:         db,
		player:     player,
		filter:     FilterAll,
		sortCol:    -1,
		selected:   -1,
		thumbCache: map[int64]fyne.Resource{},
		tickerDone: make(chan struct{}),
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

	// Apply the saved play-count threshold.
	pct := u.app.Preferences().IntWithFallback(prefPlayCountPct, defaultPlayCountPct)
	player.SetCountThreshold(float64(pct) / 100)

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

// setPlayCountPct saves the play-count threshold preference and applies it. The
// menu is rebuilt via fyne.Do (see toggleColumn for why - macOS menu tracking).
func (u *ui) setPlayCountPct(pct int) {
	u.app.Preferences().SetInt(prefPlayCountPct, pct)
	u.player.SetCountThreshold(float64(pct) / 100)
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
		u.reload()
	}

	left := container.NewHBox(addBtn, rescanBtn, widget.NewSeparator(),
		widget.NewLabel("Show:"), filterSel)

	// Search across title/artist/album/genre/filename (handled in SQL).
	search := widget.NewEntry()
	search.SetPlaceHolder("Search title, artist, album, genre, filename…")
	search.OnChanged = func(s string) {
		u.search = s
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
	u.volSlider.OnChanged = func(v float64) { u.player.SetVolume(v) }
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

	u.visibleCols = u.visibleCols[:0]
	for _, c := range allColumns {
		if c.id == colTrack && !showTrack {
			continue
		}
		if c.id == colFilename && !showFile {
			continue
		}
		u.visibleCols = append(u.visibleCols, c)
	}
	for i, c := range u.visibleCols {
		u.table.SetColumnWidth(i, c.width)
	}
	u.table.Refresh()
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
			if u.sortCol == c.id {
				if u.sortDesc {
					title += " ▼"
				} else {
					title += " ▲"
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
	if col.text != nil {
		lbl.SetText(col.text(tr))
	} else {
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
	if c.ui.visibleCols[c.id.Col].id == colRating {
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

// headerWidget is a clickable column header used to drive sorting.
type headerWidget struct {
	widget.BaseWidget
	ui    *ui
	col   int
	label *widget.Label
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

func (h *headerWidget) Tapped(_ *fyne.PointEvent) {
	if h.col < 0 || h.col >= len(h.ui.visibleCols) {
		return
	}
	h.ui.sortByColumn(h.ui.visibleCols[h.col].id) // map physical index -> logical id
}

// sortByColumn sets the sort column (toggling direction if already active) and
// reloads. The Art column isn't sortable.
func (u *ui) sortByColumn(col int) {
	if col == colArt {
		return
	}
	if u.sortCol == col {
		u.sortDesc = !u.sortDesc
	} else {
		u.sortCol = col
		u.sortDesc = false
	}
	u.reload()
	u.table.Refresh() // redraw headers with the updated sort arrow
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

	menu := fyne.NewMenu("",
		fyne.NewMenuItem("Play", func() { u.player.PlayQueue(u.tracks, r) }),
		ratingItem,
		fyne.NewMenuItemSeparator(),
		soon("Rename file…  (coming soon)"),
		soon("Edit tags…  (coming soon)"),
		soon("Add album art…  (coming soon)"),
	)
	widget.ShowPopUpMenuAtPosition(menu, u.win.Canvas(), pos)
}

// thumb returns (and caches) the album-art thumbnail resource for a track.
func (u *ui) thumb(tr Track) fyne.Resource {
	if res, ok := u.thumbCache[tr.ID]; ok {
		return res
	}
	if !tr.HasArt {
		u.thumbCache[tr.ID] = nil
		return nil
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

// reload re-queries the catalog with the current filter and refreshes the table.
func (u *ui) reload() {
	tracks, err := u.db.Tracks(TrackQuery{
		Filter:  u.filter,
		Search:  u.search,
		SortCol: u.sortCol,
		Desc:    u.sortDesc,
	})
	if err != nil {
		dialog.ShowError(err, u.win)
		return
	}
	u.tracks = tracks
	u.selected = -1
	u.thumbCache = map[int64]fyne.Resource{}
	u.table.Refresh()
	for r := range u.tracks {
		u.table.SetRowHeight(r, tableRowHeight)
	}
	u.refreshStatus()
}

func (u *ui) refreshStatus() {
	sel := ""
	if u.selected >= 0 && u.selected < len(u.tracks) {
		sel = "  |  selected: " + u.tracks[u.selected].Title
	}
	u.status.SetText(fmt.Sprintf("%d tracks%s", len(u.tracks), sel))
}

// refreshNowPlaying syncs the transport bar with the player. Runs on UI thread.
func (u *ui) refreshNowPlaying() {
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
