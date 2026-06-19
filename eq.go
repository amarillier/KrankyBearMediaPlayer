// Package main - eq.go is the 10-band graphic equalizer: a cascade of peaking-EQ
// biquad filters applied to the playback stream (player.go), driven by per-band gain
// sliders (eqwindow.go) with built-in and user-saved presets. Filters run at the fixed
// playerSampleRate, so coefficients are computed once per gain change. It reuses the
// biquad type from rgscan.go.
package main

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"github.com/gopxl/beep/v2"
)

const (
	eqBandCount = 10
	eqMinDB     = -12.0
	eqMaxDB     = 12.0
	eqQ         = 1.41 // ~one octave per band
)

// eqFreqs are the ISO octave centre frequencies (Hz) for the 10 bands.
var eqFreqs = [eqBandCount]float64{31, 62, 125, 250, 500, 1000, 2000, 4000, 8000, 16000}

// eqFreqLabels are the slider captions.
var eqFreqLabels = [eqBandCount]string{"31", "62", "125", "250", "500", "1k", "2k", "4k", "8k", "16k"}

// setPeaking sets RBJ "cookbook" peaking-EQ coefficients for a centre frequency, gain
// (dB) and Q at sample rate fs. It updates coefficients only, leaving the filter state
// intact so a live gain change doesn't pop. At 0 dB the section is an identity filter.
func (b *biquad) setPeaking(freq, gainDB, q, fs float64) {
	A := math.Pow(10, gainDB/40)
	w0 := 2 * math.Pi * freq / fs
	cw := math.Cos(w0)
	alpha := math.Sin(w0) / (2 * q)

	a0 := 1 + alpha/A
	b.b0 = (1 + alpha*A) / a0
	b.b1 = (-2 * cw) / a0
	b.b2 = (1 - alpha*A) / a0
	b.a1 = (-2 * cw) / a0
	b.a2 = (1 - alpha/A) / a0
}

// eqStreamer wraps a source stream and applies the band cascade per channel when
// enabled. The L and R filters share coefficients but keep independent state.
type eqStreamer struct {
	src     beep.Streamer
	enabled bool
	l       [eqBandCount]biquad
	r       [eqBandCount]biquad
}

// newEQStreamer builds an EQ stage from the given gains/enabled, computing coefficients
// for playerSampleRate.
func newEQStreamer(src beep.Streamer, gains [eqBandCount]float64, enabled bool) *eqStreamer {
	e := &eqStreamer{src: src, enabled: enabled}
	e.setGains(gains)
	return e
}

// setGains recomputes all band coefficients (L+R) for the given gains, preserving state.
func (e *eqStreamer) setGains(gains [eqBandCount]float64) {
	fs := float64(playerSampleRate)
	for i := 0; i < eqBandCount; i++ {
		e.l[i].setPeaking(eqFreqs[i], gains[i], eqQ, fs)
		e.r[i].setPeaking(eqFreqs[i], gains[i], eqQ, fs)
	}
}

func (e *eqStreamer) Stream(buf [][2]float64) (int, bool) {
	n, ok := e.src.Stream(buf)
	if e.enabled {
		for i := 0; i < n; i++ {
			l, r := buf[i][0], buf[i][1]
			for b := 0; b < eqBandCount; b++ {
				l = e.l[b].process(l)
				r = e.r[b].process(r)
			}
			buf[i][0], buf[i][1] = l, r
		}
	}
	return n, ok
}

func (e *eqStreamer) Err() error { return nil }

// eqPreset is a named gain curve.
type eqPreset struct {
	name  string
	gains [eqBandCount]float64
}

// eqPresets are the built-in genre/music-type presets. Bands: 31,62,125,250,500,1k,2k,4k,8k,16k.
// The first block is the original hand-made set; the second is an AIMP-derived set
// (recreated against our 10 bands), giving a broader library of "known-good" curves.
var eqPresets = []eqPreset{
	{"Flat", [eqBandCount]float64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
	{"Bass Boost", [eqBandCount]float64{6, 5, 4, 2, 0, 0, 0, 0, 0, 0}},
	{"Treble Boost", [eqBandCount]float64{0, 0, 0, 0, 0, 0, 2, 4, 5, 6}},
	{"Vocal", [eqBandCount]float64{-2, -1, 0, 2, 4, 4, 3, 1, 0, -1}},
	{"Rock", [eqBandCount]float64{5, 3, -1, -2, -1, 1, 2, 4, 4, 4}},
	{"Classical", [eqBandCount]float64{4, 3, 2, 1, -1, -1, 0, 2, 3, 4}},
	{"Pop", [eqBandCount]float64{-1, 1, 3, 4, 3, 0, -1, -1, 1, 2}},
	{"Jazz", [eqBandCount]float64{3, 2, 1, 2, -1, -1, 0, 1, 2, 3}},
	{"Dance", [eqBandCount]float64{6, 5, 2, 0, -1, -2, -1, 0, 3, 4}},
	{"Loudness", [eqBandCount]float64{6, 4, 0, -2, -3, -2, 0, 2, 5, 6}},
	// AIMP-derived presets.
	{"Acoustic Folk", [eqBandCount]float64{-2, -1, 1, 2, 3, 4, 3, 2, 1, 0}},
	{"Ballad", [eqBandCount]float64{-6, -4, 2, 4, 4, 2, 1, -3, -4, -5}},
	{"Club", [eqBandCount]float64{0, 0, 1, 2, 3, 5, 2, 1, 0, 0}},
	{"Electronic Pop", [eqBandCount]float64{5, 6, 2, 0, -1, 1, 2, 3, 4, 4}},
	{"Full Bass", [eqBandCount]float64{7, 7, 7, 4, 0, -3, -5, -6, -7, -8}},
	{"Full Bass & Treble", [eqBandCount]float64{7, 7, 7, 0, -2, 0, 2, 6, 8, 9}},
	{"Full Treble", [eqBandCount]float64{-8, -8, -8, -6, -2, 3, 8, 12, 12, 12}},
	{"Headphones", [eqBandCount]float64{7, 7, 7, 5, 0, -2, 0, 2, 4, 9}},
	{"HipHop Rap", [eqBandCount]float64{6, 5, 3, 0, -2, -1, 1, 2, 4, 5}},
	{"Large Hall", [eqBandCount]float64{7, 7, 7, 5, 4, 0, -2, -3, -3, 0}},
	{"Live", [eqBandCount]float64{-4, -3, 3, 5, 5, 4, 3, 2, 2, 1}},
	{"Metal", [eqBandCount]float64{4, 5, 4, 1, -2, -2, 1, 3, 4, 4}},
	{"Party", [eqBandCount]float64{5, 5, 0, 0, 0, 0, 0, 0, 0, 5}},
	{"Rap", [eqBandCount]float64{0, 4, 3, -4, -3, 2, -5, 3, 5, 0}},
	{"Reggae", [eqBandCount]float64{0, 0, -1, 0, -3, 0, 2, 4, 3, 0}},
	{"Ska", [eqBandCount]float64{-1, -2, -3, -3, -1, 2, 3, 4, 6, 6}},
	{"Soft", [eqBandCount]float64{4, 2, 1, -3, -1, 2, 6, 7, 8, 8}},
	{"Soft Rock", [eqBandCount]float64{2, 2, 2, 0, -3, -4, -4, -2, 2, 8}},
	{"Techno", [eqBandCount]float64{5, 5, 4, 1, -2, -3, -2, 2, 5, 6}},
}

// builtinPresetNames returns the built-in preset names in order.
func builtinPresetNames() []string {
	names := make([]string, len(eqPresets))
	for i, p := range eqPresets {
		names[i] = p.name
	}
	return names
}

// presetGains returns the gains for a named built-in preset (ok=false if unknown).
func presetGains(name string) ([eqBandCount]float64, bool) {
	for _, p := range eqPresets {
		if p.name == name {
			return p.gains, true
		}
	}
	return [eqBandCount]float64{}, false
}

// --- gain (de)serialization ---

// gainsToCSV formats band gains as a comma-separated string for prefs.
func gainsToCSV(g [eqBandCount]float64) string {
	parts := make([]string, eqBandCount)
	for i, v := range g {
		parts[i] = strconv.FormatFloat(v, 'g', -1, 64)
	}
	return strings.Join(parts, ",")
}

// gainsFromCSV parses what gainsToCSV produced; missing/garbage fields stay 0.
func gainsFromCSV(s string) [eqBandCount]float64 {
	var g [eqBandCount]float64
	if strings.TrimSpace(s) == "" {
		return g
	}
	for i, p := range strings.Split(s, ",") {
		if i >= eqBandCount {
			break
		}
		if v, err := strconv.ParseFloat(strings.TrimSpace(p), 64); err == nil {
			g[i] = clampGain(v)
		}
	}
	return g
}

func clampGain(v float64) float64 {
	if v < eqMinDB {
		return eqMinDB
	}
	if v > eqMaxDB {
		return eqMaxDB
	}
	return v
}

// --- custom presets (persisted as JSON in a preference) ---

// customPresets decodes the user's saved presets from the JSON pref value.
func customPresets(jsonStr string) map[string][eqBandCount]float64 {
	out := map[string][eqBandCount]float64{}
	if strings.TrimSpace(jsonStr) == "" {
		return out
	}
	var raw map[string][]float64
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return out
	}
	for name, vals := range raw {
		var g [eqBandCount]float64
		for i := 0; i < eqBandCount && i < len(vals); i++ {
			g[i] = clampGain(vals[i])
		}
		out[name] = g
	}
	return out
}

// encodeCustomPresets serializes custom presets back to JSON for the pref.
func encodeCustomPresets(m map[string][eqBandCount]float64) string {
	raw := map[string][]float64{}
	for name, g := range m {
		s := make([]float64, eqBandCount)
		for i, v := range g {
			s[i] = v
		}
		raw[name] = s
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return ""
	}
	return string(b)
}
