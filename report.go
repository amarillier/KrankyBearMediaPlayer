// Package main - report.go is the library report/export tool: a dialog that
// summarises the catalog (or the current filtered view) by various groupings (flat
// or two-level hierarchies), with a Name/Year sort and direction toggle, and exports
// the chosen report to CSV. Aggregation is done in Go over db.Tracks results.
package main

import (
	"encoding/csv"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"mediaplayer/internal/i18n"
)

// report group-by modes (also the dialog labels). Flat modes produce one row per
// group; hierarchical modes nest children under a top-level group.
const (
	reportByArtist    = "By Artist"
	reportByAlbum     = "By Album"
	reportByGenre     = "By Genre"
	reportTracks      = "All tracks"
	reportArtistAlbum = "Artist → Album"
	reportAlbumArtist = "Album → Artist"
	reportGenreArtist = "Genre → Artist"
)

var reportModes = []string{
	reportByArtist, reportByAlbum, reportByGenre,
	reportArtistAlbum, reportAlbumArtist, reportGenreArtist, reportTracks,
}

var reportSorts = []string{"Name", "Year"}

func orUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(unknown)"
	}
	return s
}

// ordStr / ordInt are direction-aware comparators for sort.Slice.
func ordStr(a, b string, desc bool) bool {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	if la == lb {
		return false
	}
	if desc {
		return la > lb
	}
	return la < lb
}

func ordInt(a, b int, desc bool) bool {
	if a == b {
		return false
	}
	if desc {
		return a > b
	}
	return a < b
}

// repGroup is a top-level group with its children (used by hierarchical modes).
type repGroup struct {
	name  string
	year  int // earliest child year (0 if none) - used for Year sort
	total int
	kids  []*repKid
}

type repKid struct {
	name   string
	year   int
	tracks int
}

// twoLevel aggregates tracks into top groups → children using the supplied key
// functions. childYear may return 0 when the mode has no meaningful year.
func twoLevel(tracks []Track, topName, childName func(Track) string, childYear func(Track) int) []*repGroup {
	gm := map[string]*repGroup{}
	km := map[string]map[string]*repKid{}
	for _, t := range tracks {
		tn := topName(t)
		g := gm[tn]
		if g == nil {
			g = &repGroup{name: tn}
			gm[tn] = g
			km[tn] = map[string]*repKid{}
		}
		g.total++
		cn := childName(t)
		k := km[tn][cn]
		if k == nil {
			k = &repKid{name: cn, year: childYear(t)}
			km[tn][cn] = k
			g.kids = append(g.kids, k)
		}
		k.tracks++
		if y := childYear(t); y > 0 && (g.year == 0 || y < g.year) {
			g.year = y
		}
	}
	out := make([]*repGroup, 0, len(gm))
	for _, g := range gm {
		out = append(out, g)
	}
	return out
}

// sortGroups orders the groups and each group's children by name or year.
func sortGroups(groups []*repGroup, sortBy string, desc, hasYear bool) {
	byYear := sortBy == "Year" && hasYear
	sort.Slice(groups, func(i, j int) bool {
		if byYear && groups[i].year != groups[j].year {
			return ordInt(groups[i].year, groups[j].year, desc)
		}
		return ordStr(groups[i].name, groups[j].name, desc)
	})
	for _, g := range groups {
		kids := g.kids
		sort.Slice(kids, func(i, j int) bool {
			if byYear && kids[i].year != kids[j].year {
				return ordInt(kids[i].year, kids[j].year, desc)
			}
			return ordStr(kids[i].name, kids[j].name, desc)
		})
	}
}

func yearStr(y int) string {
	if y > 0 {
		return strconv.Itoa(y)
	}
	return ""
}

// reportLevel is one grouping level for the "List tracks" view: a column label, the
// grouping key, and a per-track year (0 = the level has no meaningful year).
type reportLevel struct {
	label string
	key   func(Track) string
	year  func(Track) int
}

func noYear(Track) int { return 0 }

// levelsFor returns the grouping levels (deepest last) for a mode when tracks are to
// be listed under each group.
func levelsFor(mode string) []reportLevel {
	artist := func(t Track) string { return orUnknown(t.Artist) }
	album := func(t Track) string { return orUnknown(t.Album) }
	genre := func(t Track) string { return orUnknown(t.Genre) }
	yr := func(t Track) int { return t.Year }
	switch mode {
	case reportByArtist:
		return []reportLevel{{"Artist", artist, noYear}}
	case reportByGenre:
		return []reportLevel{{"Genre", genre, noYear}}
	case reportByAlbum:
		return []reportLevel{{"Album", func(t Track) string {
			return orUnknown(t.Artist) + " — " + orUnknown(t.Album)
		}, yr}}
	case reportArtistAlbum:
		return []reportLevel{{"Artist", artist, noYear}, {"Album", album, yr}}
	case reportAlbumArtist:
		return []reportLevel{{"Album", album, yr}, {"Artist", artist, noYear}}
	case reportGenreArtist:
		return []reportLevel{{"Genre", genre, noYear}, {"Artist", artist, noYear}}
	}
	return nil
}

// renderGroupedTracks builds a grouped report that lists the tracks under each
// (deepest) group, honouring the Name/Year sort + direction. CSV rows are one per
// track with the grouping columns as context; display is an indented tree.
func renderGroupedTracks(tracks []Track, levels []reportLevel, sortBy string, desc bool) (header []string, rows [][]string, display []string) {
	for _, l := range levels {
		header = append(header, l.label)
	}
	header = append(header, "Track", "Title", "Year")

	var rec func(ts []Track, lv []reportLevel, depth int, anc []string)
	rec = func(ts []Track, lv []reportLevel, depth int, anc []string) {
		if len(lv) == 0 { // leaf: list the tracks
			leaf := append([]Track(nil), ts...)
			sort.Slice(leaf, func(i, j int) bool {
				a, b := leaf[i], leaf[j]
				if sortBy == "Year" && a.Year != b.Year {
					return ordInt(a.Year, b.Year, desc)
				}
				if a.TrackNo != b.TrackNo {
					return a.TrackNo < b.TrackNo
				}
				return ordStr(a.Title, b.Title, desc)
			})
			ind := strings.Repeat("    ", depth)
			for _, t := range leaf {
				no := ""
				if t.TrackNo > 0 {
					no = fmt.Sprintf("%02d", t.TrackNo)
				}
				rows = append(rows, append(append([]string{}, anc...), no, orUnknown(t.Title), yearStr(t.Year)))
				display = append(display, strings.TrimRight(fmt.Sprintf("%s%s %s", ind, no, orUnknown(t.Title)), " "))
			}
			return
		}
		l := lv[0]
		groups := map[string][]Track{}
		var order []string
		gyear := map[string]int{}
		for _, t := range ts {
			k := l.key(t)
			if _, ok := groups[k]; !ok {
				order = append(order, k)
			}
			groups[k] = append(groups[k], t)
			if y := l.year(t); y > 0 && (gyear[k] == 0 || y < gyear[k]) {
				gyear[k] = y
			}
		}
		sort.Slice(order, func(i, j int) bool {
			if sortBy == "Year" && gyear[order[i]] != gyear[order[j]] {
				return ordInt(gyear[order[i]], gyear[order[j]], desc)
			}
			return ordStr(order[i], order[j], desc)
		})
		ind := strings.Repeat("    ", depth)
		for _, k := range order {
			sub := groups[k]
			display = append(display, fmt.Sprintf("%s%s  (%d)", ind, k, len(sub)))
			rec(sub, lv[1:], depth+1, append(append([]string{}, anc...), k))
		}
	}
	rec(tracks, levels, 0, nil)
	return header, rows, display
}

// buildReport aggregates tracks for the chosen mode + sort, returning the CSV header,
// the CSV data rows, the indented display lines (for the on-screen list), and a
// one-line summary. Pure (no UI) so it's unit-testable.
func buildReport(tracks []Track, mode, sortBy string, desc, listTracks bool) (header []string, rows [][]string, display []string, summary string) {
	artists := map[string]bool{}
	albums := map[string]bool{}
	for _, t := range tracks {
		artists[strings.ToLower(orUnknown(t.Artist))] = true
		albums[strings.ToLower(orUnknown(t.Artist))+"\x00"+strings.ToLower(orUnknown(t.Album))] = true
	}
	summary = fmt.Sprintf("%d artists · %d albums · %d tracks", len(artists), len(albums), len(tracks))

	// "List tracks" expands every grouping to show its tracks (All tracks already is
	// a track listing, so it ignores the flag).
	if listTracks && mode != reportTracks {
		if levels := levelsFor(mode); levels != nil {
			header, rows, display = renderGroupedTracks(tracks, levels, sortBy, desc)
			return header, rows, display, summary
		}
	}

	switch mode {
	case reportArtistAlbum, reportAlbumArtist, reportGenreArtist:
		var topName, childName func(Track) string
		var childYear func(Track) int
		hasYear := true
		switch mode {
		case reportArtistAlbum:
			header = []string{"Artist", "Album", "Year", "Tracks"}
			topName = func(t Track) string { return orUnknown(t.Artist) }
			childName = func(t Track) string { return orUnknown(t.Album) }
			childYear = func(t Track) int { return t.Year }
		case reportAlbumArtist:
			header = []string{"Album", "Artist", "Year", "Tracks"}
			topName = func(t Track) string { return orUnknown(t.Album) }
			childName = func(t Track) string { return orUnknown(t.Artist) }
			childYear = func(t Track) int { return t.Year }
		default: // reportGenreArtist
			header = []string{"Genre", "Artist", "Tracks"}
			topName = func(t Track) string { return orUnknown(t.Genre) }
			childName = func(t Track) string { return orUnknown(t.Artist) }
			childYear = func(t Track) int { return 0 }
			hasYear = false
		}
		groups := twoLevel(tracks, topName, childName, childYear)
		sortGroups(groups, sortBy, desc, hasYear)
		for _, g := range groups {
			display = append(display, fmt.Sprintf("%s  (%d)", g.name, g.total))
			for _, k := range g.kids {
				if hasYear {
					y := yearStr(k.year)
					rows = append(rows, []string{g.name, k.name, y, strconv.Itoa(k.tracks)})
					line := "    " + k.name
					if y != "" {
						line += "  (" + y + ")"
					}
					display = append(display, fmt.Sprintf("%s  — %d", line, k.tracks))
				} else {
					rows = append(rows, []string{g.name, k.name, strconv.Itoa(k.tracks)})
					display = append(display, fmt.Sprintf("    %s  — %d", k.name, k.tracks))
				}
			}
		}
		return header, rows, display, summary
	}

	// Flat modes.
	switch mode {
	case reportByArtist:
		header = []string{"Artist", "Albums", "Tracks"}
		type agg struct {
			albums map[string]bool
			tracks int
		}
		m := map[string]*agg{}
		for _, t := range tracks {
			a := orUnknown(t.Artist)
			if m[a] == nil {
				m[a] = &agg{albums: map[string]bool{}}
			}
			m[a].albums[strings.ToLower(orUnknown(t.Album))] = true
			m[a].tracks++
		}
		for a, g := range m {
			rows = append(rows, []string{a, strconv.Itoa(len(g.albums)), strconv.Itoa(g.tracks)})
		}
		sort.Slice(rows, func(i, j int) bool { return ordStr(rows[i][0], rows[j][0], desc) })

	case reportByGenre:
		header = []string{"Genre", "Tracks"}
		m := map[string]int{}
		for _, t := range tracks {
			m[orUnknown(t.Genre)]++
		}
		for g, n := range m {
			rows = append(rows, []string{g, strconv.Itoa(n)})
		}
		sort.Slice(rows, func(i, j int) bool { return ordStr(rows[i][0], rows[j][0], desc) })

	case reportByAlbum:
		header = []string{"Artist", "Album", "Year", "Tracks"}
		type agg struct {
			artist, album string
			year, tracks  int
		}
		m := map[string]*agg{}
		for _, t := range tracks {
			key := strings.ToLower(orUnknown(t.Artist)) + "\x00" + strings.ToLower(orUnknown(t.Album))
			if m[key] == nil {
				m[key] = &agg{artist: orUnknown(t.Artist), album: orUnknown(t.Album), year: t.Year}
			}
			m[key].tracks++
		}
		type albRow struct {
			artist, album string
			year, tracks  int
		}
		var albs []albRow
		for _, g := range m {
			albs = append(albs, albRow{g.artist, g.album, g.year, g.tracks})
		}
		byYear := sortBy == "Year"
		sort.Slice(albs, func(i, j int) bool {
			if byYear && albs[i].year != albs[j].year {
				return ordInt(albs[i].year, albs[j].year, desc)
			}
			if !strings.EqualFold(albs[i].artist, albs[j].artist) {
				return ordStr(albs[i].artist, albs[j].artist, desc)
			}
			return ordStr(albs[i].album, albs[j].album, desc)
		})
		for _, a := range albs {
			rows = append(rows, []string{a.artist, a.album, yearStr(a.year), strconv.Itoa(a.tracks)})
		}

	default: // reportTracks
		header = []string{"Artist", "Album", "Track", "Title"}
		sorted := append([]Track(nil), tracks...)
		byYear := sortBy == "Year"
		sort.Slice(sorted, func(i, j int) bool {
			a, b := sorted[i], sorted[j]
			if byYear && a.Year != b.Year {
				return ordInt(a.Year, b.Year, desc)
			}
			if !strings.EqualFold(a.Artist, b.Artist) {
				return ordStr(a.Artist, b.Artist, desc)
			}
			if !strings.EqualFold(a.Album, b.Album) {
				return ordStr(a.Album, b.Album, desc)
			}
			return a.TrackNo < b.TrackNo
		})
		for _, t := range sorted {
			track := ""
			if t.TrackNo > 0 {
				track = strconv.Itoa(t.TrackNo)
			}
			rows = append(rows, []string{orUnknown(t.Artist), orUnknown(t.Album), track, orUnknown(t.Title)})
		}
	}

	for _, r := range rows {
		display = append(display, strings.Join(r, "   |   "))
	}
	return header, rows, display, summary
}

// showLibraryReport opens the report dialog (Library → Library Report…).
func (u *ui) showLibraryReport() {
	// The report's group-by/sort/source values double as internal logic keys (used in
	// switches and CSV filenames), so the Selects show translated labels mapped back to
	// the stable English keys. The report BODY/CSV headers stay English for now.
	srcWhole, srcView := i18n.T("report.source_whole"), i18n.T("report.source_view")
	sourceSel := widget.NewRadioGroup([]string{srcWhole, srcView}, nil)
	sourceSel.SetSelected(srcWhole)
	sourceSel.Horizontal = true

	modeLabelByKey := map[string]string{
		reportByArtist:    i18n.T("report.mode_by_artist"),
		reportByAlbum:     i18n.T("report.mode_by_album"),
		reportByGenre:     i18n.T("report.mode_by_genre"),
		reportArtistAlbum: i18n.T("report.mode_artist_album"),
		reportAlbumArtist: i18n.T("report.mode_album_artist"),
		reportGenreArtist: i18n.T("report.mode_genre_artist"),
		reportTracks:      i18n.T("report.mode_all_tracks"),
	}
	modeKeyByLabel := map[string]string{}
	modeLabels := make([]string, len(reportModes))
	for i, k := range reportModes {
		modeLabels[i] = modeLabelByKey[k]
		modeKeyByLabel[modeLabelByKey[k]] = k
	}
	modeSel := widget.NewSelect(modeLabels, nil)
	modeSel.SetSelected(modeLabelByKey[reportByArtist])
	modeKey := func() string { return modeKeyByLabel[modeSel.Selected] }

	sortLabelByKey := map[string]string{"Name": i18n.T("report.sort_name"), "Year": i18n.T("report.sort_year")}
	sortKeyByLabel := map[string]string{}
	sortLabels := make([]string, len(reportSorts))
	for i, k := range reportSorts {
		sortLabels[i] = sortLabelByKey[k]
		sortKeyByLabel[sortLabelByKey[k]] = k
	}
	sortSel := widget.NewSelect(sortLabels, nil)
	sortSel.SetSelected(sortLabelByKey["Name"])

	descChk := widget.NewCheck(i18n.T("report.descending"), nil)
	tracksChk := widget.NewCheck(i18n.T("report.list_tracks"), nil)

	summary := widget.NewLabel("")
	var header []string
	var rows [][]string
	var display []string

	list := widget.NewList(
		func() int { return len(display) },
		func() fyne.CanvasObject {
			l := widget.NewLabel("")
			l.Truncation = fyne.TextTruncateEllipsis
			return l
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			o.(*widget.Label).SetText(display[i])
		},
	)

	rebuild := func() {
		var tracks []Track
		if sourceSel.Selected == srcView {
			tracks = append([]Track(nil), u.tracks...)
		} else {
			t, err := u.db.Tracks(TrackQuery{})
			if err != nil {
				dialog.ShowError(err, u.win)
				return
			}
			tracks = t
		}
		var s string
		header, rows, display, s = buildReport(tracks, modeKey(), sortKeyByLabel[sortSel.Selected], descChk.Checked, tracksChk.Checked)
		summary.SetText(strings.Join(header, "   |   ") + "\n" + s)
		list.Refresh()
		list.ScrollToTop()
	}
	// "List tracks" is meaningless for the flat All-tracks listing.
	modeSel.OnChanged = func(string) {
		if modeKey() == reportTracks {
			tracksChk.Disable()
		} else {
			tracksChk.Enable()
		}
		rebuild()
	}
	sourceSel.OnChanged = func(string) { rebuild() }
	sortSel.OnChanged = func(string) { rebuild() }
	descChk.OnChanged = func(bool) { rebuild() }
	tracksChk.OnChanged = func(bool) { rebuild() }
	rebuild()

	exportBtn := widget.NewButton(i18n.T("report.export_csv"), func() {
		u.exportReportCSV(header, rows, modeKey())
	})

	controls := container.NewVBox(
		container.NewBorder(nil, nil, widget.NewLabel(i18n.T("report.source")), tracksChk, sourceSel),
		container.NewGridWithColumns(2,
			container.NewBorder(nil, nil, widget.NewLabel(i18n.T("report.group_by")), nil, modeSel),
			container.NewBorder(nil, nil, widget.NewLabel(i18n.T("report.sort")), descChk, sortSel),
		),
		container.NewBorder(nil, nil, nil, exportBtn, summary),
		widget.NewSeparator(),
	)
	body := container.NewBorder(controls, nil, nil, nil, list)
	d := dialog.NewCustom(i18n.T("report.title"), i18n.T("common.close"), body, u.win)
	d.Resize(fyne.NewSize(720, 640))
	d.Show()
}

// exportReportCSV writes the current report (header + rows) to a user-chosen CSV file.
func (u *ui) exportReportCSV(header []string, rows [][]string, mode string) {
	fd := dialog.NewFileSave(func(w fyne.URIWriteCloser, err error) {
		if err != nil || w == nil {
			return
		}
		defer w.Close()
		cw := csv.NewWriter(w)
		_ = cw.Write(header)
		_ = cw.WriteAll(rows)
		cw.Flush()
		if err := cw.Error(); err != nil {
			dialog.ShowError(err, u.win)
			return
		}
		u.status.SetText(i18n.TC("report.exported", map[string]string{"n": fmt.Sprintf("%d", len(rows)), "file": w.URI().Name()}))
	}, u.win)
	name := "library-report-" + strings.ToLower(strings.ReplaceAll(mode, " ", "-")) + ".csv"
	name = strings.ReplaceAll(name, "→", "to")
	fd.SetFileName(name)
	fd.Show()
}
