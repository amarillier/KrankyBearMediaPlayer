// Package main - preferences.go is the consolidated Preferences dialog, gathering
// settings that were previously scattered across menus (theme, play-count
// threshold, optional columns) plus the catalog database location and the
// remembered playback volume. Changes apply on Save; Cancel discards them.
package main

import (
	"fmt"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// playCountOptions maps the play-count threshold dropdown labels to percentages.
var playCountOptions = []struct {
	Label string
	Pct   int
}{
	{"25%", 25},
	{"50%", 50},
	{"75%", 75},
	{"90%", 90},
	{"End of track (100%)", 100},
}

func playCountLabelFor(pct int) string {
	for _, o := range playCountOptions {
		if o.Pct == pct {
			return o.Label
		}
	}
	return "50%"
}

func playCountPctFor(label string) int {
	for _, o := range playCountOptions {
		if o.Label == label {
			return o.Pct
		}
	}
	return defaultPlayCountPct
}

// showPreferences opens the consolidated Preferences dialog. Settings apply only
// when Save is pressed (Database location changes apply immediately but need a
// restart, which is called out inline).
func (u *ui) showPreferences() {
	prefs := u.app.Preferences()

	// --- Appearance: theme ---
	themeSel := widget.NewSelect([]string{"System", "Light", "Dark"}, nil)
	switch prefs.StringWithFallback("theme", "system") {
	case "light":
		themeSel.SetSelected("Light")
	case "dark":
		themeSel.SetSelected("Dark")
	default:
		themeSel.SetSelected("System")
	}

	// --- Playback: count-play threshold + remembered volume ---
	pctSel := widget.NewSelect(playCountLabels(), nil)
	pctSel.SetSelected(playCountLabelFor(prefs.IntWithFallback(prefPlayCountPct, defaultPlayCountPct)))

	volSlider := widget.NewSlider(0, 1)
	volSlider.Step = 0.01
	volSlider.Value = u.player.Volume()
	volPct := widget.NewLabel(fmt.Sprintf("%d%%", int(volSlider.Value*100+0.5)))
	volSlider.OnChanged = func(v float64) { volPct.SetText(fmt.Sprintf("%d%%", int(v*100+0.5))) }
	volRow := container.NewBorder(nil, nil, nil, volPct, volSlider)

	// --- Library: optional columns ---
	trackChk := widget.NewCheck("Track # column", nil)
	trackChk.Checked = prefs.BoolWithFallback(prefShowTrackCol, false)
	fileChk := widget.NewCheck("Filename column", nil)
	fileChk.Checked = prefs.BoolWithFallback(prefShowFilenameCol, false)
	selChk := widget.NewCheck("Selection checkboxes", nil)
	selChk.Checked = prefs.BoolWithFallback(prefShowSelectCol, false)

	// --- Library: database location ---
	dbPathLabel := widget.NewLabel(resolveDBPath(u.app))
	dbPathLabel.Wrapping = fyne.TextWrapBreak
	restartNote := func() {
		dialog.ShowInformation("Database location",
			"The catalog will use:\n\n"+resolveDBPath(u.app)+
				"\n\nRestart "+appName+" for the change to take effect.", u.win)
	}
	dbChangeBtn := widget.NewButton("Change folder…", func() {
		dialog.ShowFolderOpen(func(list fyne.ListableURI, err error) {
			if err != nil || list == nil {
				return
			}
			prefs.SetString(prefDBPath, filepath.Join(list.Path(), defaultDBName))
			dbPathLabel.SetText(resolveDBPath(u.app))
			restartNote()
		}, u.win)
	})
	dbResetBtn := widget.NewButton("Use default", func() {
		prefs.RemoveValue(prefDBPath)
		dbPathLabel.SetText(resolveDBPath(u.app))
		restartNote()
	})
	dbNote := widget.NewLabel("The library is a single SQLite file; keep it beside the app on a " +
		"USB drive for a fully portable setup. (The -db flag and KBMP_DB override this.)")
	dbNote.Wrapping = fyne.TextWrapWord

	section := func(title string) fyne.CanvasObject {
		return container.NewVBox(
			widget.NewSeparator(),
			widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		)
	}

	body := container.NewVBox(
		section("Appearance"),
		widget.NewForm(widget.NewFormItem("Theme", themeSel)),
		section("Playback"),
		widget.NewForm(
			widget.NewFormItem("Count a play after", pctSel),
			widget.NewFormItem("Volume", volRow),
		),
		section("Library columns"),
		trackChk, fileChk, selChk,
		section("Database location"),
		dbPathLabel,
		container.NewHBox(dbChangeBtn, dbResetBtn),
		dbNote,
	)

	scroll := container.NewVScroll(body)
	scroll.SetMinSize(fyne.NewSize(460, 460))

	d := dialog.NewCustomConfirm("Preferences", "Save", "Cancel", scroll, func(ok bool) {
		if !ok {
			return
		}
		// Theme.
		switch themeSel.Selected {
		case "Light":
			setLightTheme(u.app)
		case "Dark":
			setDarkTheme(u.app)
		default:
			setSystemTheme(u.app)
		}
		// Play-count threshold.
		pct := playCountPctFor(pctSel.Selected)
		prefs.SetInt(prefPlayCountPct, pct)
		u.player.SetCountThreshold(float64(pct) / 100)
		// Volume.
		u.player.SetVolume(volSlider.Value)
		prefs.SetFloat(prefVolume, volSlider.Value)
		u.volSlider.SetValue(volSlider.Value)
		// Optional columns.
		prefs.SetBool(prefShowTrackCol, trackChk.Checked)
		prefs.SetBool(prefShowFilenameCol, fileChk.Checked)
		prefs.SetBool(prefShowSelectCol, selChk.Checked)
		u.rebuildColumns()
		u.rebuildMenu() // refresh the View menu checkmarks to match
	}, u.win)
	d.Resize(fyne.NewSize(520, 560))
	d.Show()
}

// playCountLabels returns just the threshold dropdown labels.
func playCountLabels() []string {
	out := make([]string, len(playCountOptions))
	for i, o := range playCountOptions {
		out[i] = o.Label
	}
	return out
}
