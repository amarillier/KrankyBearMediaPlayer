// Package main - eqwindow.go is the equalizer window (Playback → Equalizer…): an
// enable toggle, a preset selector (built-in + user-saved), ten vertical band sliders,
// and Save/Delete/Reset. Changes apply live via Player.SetEQ and persist to prefs.
package main

import (
	"fmt"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

var eqWindow fyne.Window

// dbCaption formats a band gain for the slider label ("0", "+3", "-2").
func dbCaption(v float64) string {
	n := int(v)
	if n == 0 {
		return "0"
	}
	return fmt.Sprintf("%+d", n)
}

// showEqualizer opens (or re-focuses) the equalizer window. It's a reused singleton,
// so its widgets stay in sync with the only thing that changes them (this window).
func (u *ui) showEqualizer() {
	if eqWindow != nil {
		eqWindow.Show()
		eqWindow.RequestFocus()
		markWindowOpen(eqWindow)
		return
	}

	prefs := u.app.Preferences()
	gains := gainsFromCSV(prefs.String(prefEQGains))

	win := u.app.NewWindow(appName + " - Equalizer")
	win.SetIcon(resourceKrankyBearMediaPlayerPng)
	eqWindow = win

	sliders := make([]*widget.Slider, eqBandCount)
	dbLabels := make([]*widget.Label, eqBandCount)
	var presetSel *widget.Select
	applyingPreset := false

	enableChk := widget.NewCheck("Enable equalizer", nil)
	enableChk.SetChecked(prefs.BoolWithFallback(prefEQEnabled, false))

	gather := func() [eqBandCount]float64 {
		var g [eqBandCount]float64
		for i, s := range sliders {
			g[i] = s.Value
		}
		return g
	}
	apply := func() { u.player.SetEQ(enableChk.Checked, gather()) }
	persist := func() {
		prefs.SetString(prefEQGains, gainsToCSV(gather()))
		prefs.SetBool(prefEQEnabled, enableChk.Checked)
	}

	cols := make([]fyne.CanvasObject, eqBandCount)
	for i := 0; i < eqBandCount; i++ {
		s := widget.NewSlider(eqMinDB, eqMaxDB)
		s.Orientation = widget.Vertical
		s.Step = 1
		s.SetValue(gains[i])
		lbl := widget.NewLabelWithStyle(dbCaption(gains[i]), fyne.TextAlignCenter, fyne.TextStyle{})
		freq := widget.NewLabelWithStyle(eqFreqLabels[i], fyne.TextAlignCenter, fyne.TextStyle{})
		idx := i
		s.OnChanged = func(v float64) {
			dbLabels[idx].SetText(dbCaption(v))
			apply()
			if !applyingPreset && presetSel != nil {
				presetSel.ClearSelected() // moved off a named preset
			}
		}
		s.OnChangeEnded = func(float64) { persist() }
		sliders[i], dbLabels[i] = s, lbl
		cols[i] = container.NewBorder(lbl, freq, nil, nil, s)
	}
	bands := container.NewGridWithColumns(eqBandCount, cols...)

	// setGains drives the sliders from a preset/reset without firing the
	// preset-clearing path on each slider.
	setGains := func(g [eqBandCount]float64) {
		applyingPreset = true
		for i, s := range sliders {
			s.SetValue(g[i])
			dbLabels[i].SetText(dbCaption(g[i]))
		}
		applyingPreset = false
		apply()
		persist()
	}

	// Preset list: "Flat" pinned first, everything else (built-in + custom) sorted
	// alphabetically (case-insensitive).
	presetOptions := func() []string {
		var rest []string
		for _, p := range eqPresets {
			if p.name != "Flat" {
				rest = append(rest, p.name)
			}
		}
		for n := range customPresets(prefs.String(prefEQCustom)) {
			rest = append(rest, n)
		}
		sort.Slice(rest, func(i, j int) bool {
			return strings.ToLower(rest[i]) < strings.ToLower(rest[j])
		})
		return append([]string{"Flat"}, rest...)
	}
	presetSel = widget.NewSelect(presetOptions(), func(name string) {
		if name == "" {
			return
		}
		if g, ok := presetGains(name); ok {
			setGains(g)
		} else if g, ok := customPresets(prefs.String(prefEQCustom))[name]; ok {
			setGains(g)
		}
		prefs.SetString(prefEQPreset, name)
	})
	presetSel.PlaceHolder = "Custom"
	if p := prefs.String(prefEQPreset); p != "" {
		presetSel.Selected = p // reflect last choice without re-applying
	}

	enableChk.OnChanged = func(bool) {
		apply()
		persist()
	}

	saveBtn := widget.NewButton("Save preset…", func() {
		entry := widget.NewEntry()
		entry.SetPlaceHolder("Preset name")
		d := dialog.NewForm("Save EQ preset", "Save", "Cancel",
			[]*widget.FormItem{widget.NewFormItem("Name", entry)},
			func(ok bool) {
				if !ok {
					return
				}
				name := strings.TrimSpace(entry.Text)
				if name == "" {
					return
				}
				cps := customPresets(prefs.String(prefEQCustom))
				cps[name] = gather()
				prefs.SetString(prefEQCustom, encodeCustomPresets(cps))
				presetSel.Options = presetOptions()
				presetSel.SetSelected(name)
				presetSel.Refresh()
			}, win)
		d.Resize(fyne.NewSize(420, 180))
		d.Show()
	})
	delBtn := widget.NewButton("Delete preset", func() {
		name := presetSel.Selected
		if _, builtin := presetGains(name); name == "" || builtin {
			dialog.ShowInformation("Delete preset",
				"Pick one of your saved custom presets to delete.", win)
			return
		}
		cps := customPresets(prefs.String(prefEQCustom))
		delete(cps, name)
		prefs.SetString(prefEQCustom, encodeCustomPresets(cps))
		presetSel.Options = presetOptions()
		presetSel.ClearSelected()
		presetSel.Refresh()
	})
	resetBtn := widget.NewButton("Reset (Flat)", func() {
		setGains([eqBandCount]float64{})
		presetSel.SetSelected("Flat")
	})

	top := container.NewVBox(
		enableChk,
		container.NewBorder(nil, nil, widget.NewLabel("Preset:"),
			container.NewHBox(saveBtn, delBtn), presetSel),
		widget.NewSeparator(),
	)
	bottom := container.NewBorder(nil, nil, resetBtn, widget.NewButton("Close", func() {
		win.Hide()
		markWindowClosed(win)
	}), nil)
	win.SetContent(container.NewBorder(top, bottom, nil, nil, bands))
	win.Resize(fyne.NewSize(600, 440))
	win.SetCloseIntercept(func() {
		win.Hide()
		markWindowClosed(win)
	})
	win.Show()
	markWindowOpen(win)
}
