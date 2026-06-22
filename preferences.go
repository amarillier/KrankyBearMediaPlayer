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

	"mediaplayer/internal/i18n"
)

// playCountThresholds is the ordered play-count threshold dropdown, by percentage.
// The plain percentages stay literal (locale-neutral numbers); only the 100% entry
// has prose, translated at render time via preferences.count_end_of_track.
var playCountThresholds = []int{25, 50, 75, 90, 100}

// playCountLabelFor returns the (translated) dropdown label for a percentage.
func playCountLabelFor(pct int) string {
	if pct == 100 {
		return i18n.T("preferences.count_end_of_track")
	}
	return fmt.Sprintf("%d%%", pct)
}

func playCountPctFor(label string) int {
	for _, pct := range playCountThresholds {
		if playCountLabelFor(pct) == label {
			return pct
		}
	}
	return defaultPlayCountPct
}

// showPreferences opens the consolidated Preferences dialog. Settings apply only
// when Save is pressed (Database location changes apply immediately but need a
// restart, which is called out inline).
func (u *ui) showPreferences() {
	prefs := u.app.Preferences()

	// --- Appearance: theme --- (Select shows translated labels mapped to stable keys)
	themeKeys := []string{"system", "light", "dark"}
	themeLabelByKey := map[string]string{
		"system": i18n.T("preferences.theme_system"),
		"light":  i18n.T("preferences.theme_light"),
		"dark":   i18n.T("preferences.theme_dark"),
	}
	themeKeyByLabel := map[string]string{}
	themeLabels := make([]string, len(themeKeys))
	for i, k := range themeKeys {
		themeLabels[i] = themeLabelByKey[k]
		themeKeyByLabel[themeLabelByKey[k]] = k
	}
	themeSel := widget.NewSelect(themeLabels, nil)
	curTheme := prefs.StringWithFallback("theme", "system")
	if _, ok := themeLabelByKey[curTheme]; !ok {
		curTheme = "system"
	}
	themeSel.SetSelected(themeLabelByKey[curTheme])

	// --- Appearance: UI language (applied on restart) ---
	langLabels, langCodeByLabel := uiLanguageOptions()
	curLangCode := i18n.NormalizeLocale(prefs.StringWithFallback(i18n.PrefKeyUILanguage, "en"))
	langSel := widget.NewSelect(langLabels, nil)
	langSel.SetSelected(i18n.LocaleDisplayName(curLangCode))

	// --- Playback: count-play threshold + remembered volume ---
	pctSel := widget.NewSelect(playCountLabels(), nil)
	pctSel.SetSelected(playCountLabelFor(prefs.IntWithFallback(prefPlayCountPct, defaultPlayCountPct)))

	volSlider := widget.NewSlider(0, 1)
	volSlider.Step = 0.01
	volSlider.Value = u.player.Volume()
	volPct := widget.NewLabel(fmt.Sprintf("%d%%", int(volSlider.Value*100+0.5)))
	volSlider.OnChanged = func(v float64) { volPct.SetText(fmt.Sprintf("%d%%", int(v*100+0.5))) }
	volRow := container.NewBorder(nil, nil, nil, volPct, volSlider)

	// --- Playback: open equalizer on launch ---
	eqLaunchChk := widget.NewCheck(i18n.T("preferences.eq_launch"), nil)
	eqLaunchChk.Checked = prefs.BoolWithFallback(prefShowEQAtLaunch, false)

	// --- Library: optional columns ---
	trackChk := widget.NewCheck(i18n.T("preferences.col_track"), nil)
	trackChk.Checked = prefs.BoolWithFallback(prefShowTrackCol, false)
	fileChk := widget.NewCheck(i18n.T("preferences.col_filename"), nil)
	fileChk.Checked = prefs.BoolWithFallback(prefShowFilenameCol, false)
	selChk := widget.NewCheck(i18n.T("preferences.col_select"), nil)
	selChk.Checked = prefs.BoolWithFallback(prefShowSelectCol, false)
	durChk := widget.NewCheck(i18n.T("preferences.col_length"), nil)
	durChk.Checked = prefs.BoolWithFallback(prefShowDurationCol, true)
	fmtChk := widget.NewCheck(i18n.T("preferences.col_format"), nil)
	fmtChk.Checked = prefs.BoolWithFallback(prefShowFormatCol, false)
	brChk := widget.NewCheck(i18n.T("preferences.col_bitrate"), nil)
	brChk.Checked = prefs.BoolWithFallback(prefShowBitrateCol, false)
	genreChk := widget.NewCheck(i18n.T("preferences.col_genre"), nil)
	genreChk.Checked = prefs.BoolWithFallback(prefShowGenreCol, false)
	colFilterChk := widget.NewCheck(i18n.T("preferences.col_filters"), nil)
	colFilterChk.Checked = prefs.BoolWithFallback(prefShowColFilters, false)

	// --- Library: database location ---
	dbPathLabel := widget.NewLabel(resolveDBPath(u.app))
	dbPathLabel.Wrapping = fyne.TextWrapBreak
	restartNote := func() {
		dialog.ShowInformation(i18n.T("preferences.db_restart_title"),
			i18n.TC("preferences.db_restart_msg", map[string]string{"path": resolveDBPath(u.app), "app": appName}), u.win)
	}
	dbChangeBtn := widget.NewButton(i18n.T("preferences.db_change"), func() {
		dialog.ShowFolderOpen(func(list fyne.ListableURI, err error) {
			if err != nil || list == nil {
				return
			}
			prefs.SetString(prefDBPath, filepath.Join(list.Path(), defaultDBName))
			dbPathLabel.SetText(resolveDBPath(u.app))
			restartNote()
		}, u.win)
	})
	dbResetBtn := widget.NewButton(i18n.T("preferences.db_default"), func() {
		prefs.RemoveValue(prefDBPath)
		dbPathLabel.SetText(resolveDBPath(u.app))
		restartNote()
	})
	dbNote := widget.NewLabel(i18n.T("preferences.db_note"))
	dbNote.Wrapping = fyne.TextWrapWord

	section := func(title string) fyne.CanvasObject {
		return container.NewVBox(
			widget.NewSeparator(),
			widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		)
	}

	body := container.NewVBox(
		section(i18n.T("preferences.section_appearance")),
		widget.NewForm(
			widget.NewFormItem(i18n.T("preferences.theme_label"), themeSel),
			widget.NewFormItem(i18n.T("preferences.language_label"), langSel),
		),
		section(i18n.T("preferences.section_playback")),
		widget.NewForm(
			widget.NewFormItem(i18n.T("preferences.count_label"), pctSel),
			widget.NewFormItem(i18n.T("preferences.volume_label"), volRow),
		),
		eqLaunchChk,
		section(i18n.T("preferences.section_columns")),
		durChk, fmtChk, brChk, genreChk, trackChk, fileChk, selChk, colFilterChk,
		section(i18n.T("preferences.section_db")),
		dbPathLabel,
		container.NewHBox(dbChangeBtn, dbResetBtn),
		dbNote,
	)

	scroll := container.NewVScroll(body)
	scroll.SetMinSize(fyne.NewSize(460, 460))

	d := dialog.NewCustomConfirm(i18n.T("preferences.title"), i18n.T("common.save"), i18n.T("common.cancel"), scroll, func(ok bool) {
		if !ok {
			return
		}
		// Theme (map the translated selection back to its stable key).
		switch themeKeyByLabel[themeSel.Selected] {
		case "light":
			setLightTheme(u.app)
		case "dark":
			setDarkTheme(u.app)
		default:
			setSystemTheme(u.app)
		}
		// UI language (applies on next launch; a live menu/window rebuild risks the
		// macOS SetMainMenu crash, so we persist and prompt for a restart instead).
		if newCode := langCodeByLabel[langSel.Selected]; newCode != "" && newCode != curLangCode {
			prefs.SetString(i18n.PrefKeyUILanguage, newCode)
			dialog.ShowInformation(i18n.T("preferences.language_label"),
				i18n.TC("preferences.language_restart_note", map[string]string{"app_name": appName}), u.win)
		}
		// Play-count threshold.
		pct := playCountPctFor(pctSel.Selected)
		prefs.SetInt(prefPlayCountPct, pct)
		u.player.SetCountThreshold(float64(pct) / 100)
		// Volume.
		u.player.SetVolume(volSlider.Value)
		prefs.SetFloat(prefVolume, volSlider.Value)
		u.volSlider.SetValue(volSlider.Value)
		// Open equalizer on launch.
		prefs.SetBool(prefShowEQAtLaunch, eqLaunchChk.Checked)
		// Optional columns.
		prefs.SetBool(prefShowTrackCol, trackChk.Checked)
		prefs.SetBool(prefShowFilenameCol, fileChk.Checked)
		prefs.SetBool(prefShowSelectCol, selChk.Checked)
		prefs.SetBool(prefShowDurationCol, durChk.Checked)
		prefs.SetBool(prefShowFormatCol, fmtChk.Checked)
		prefs.SetBool(prefShowBitrateCol, brChk.Checked)
		prefs.SetBool(prefShowGenreCol, genreChk.Checked)
		prefs.SetBool(prefShowColFilters, colFilterChk.Checked)
		u.applyColFilterVisibility() // show/hide the filter row to match
		u.rebuildColumns()
		u.rebuildMenu() // refresh the View menu checkmarks to match
	}, u.win)
	d.Resize(fyne.NewSize(520, 560))
	d.Show()
}

// playCountLabels returns the (translated) threshold dropdown labels.
func playCountLabels() []string {
	out := make([]string, len(playCountThresholds))
	for i, pct := range playCountThresholds {
		out[i] = playCountLabelFor(pct)
	}
	return out
}
