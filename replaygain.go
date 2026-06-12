// Package main - replaygain.go reads ReplayGain track tags and converts them to a
// gain offset for the playback volume chain (player.go). ReplayGain stores, per
// track, how many dB to adjust playback so everything plays at a consistent
// loudness; an optional peak value lets us cap the gain so boosted (quiet) tracks
// don't clip. Reading is uniform via dhowden/tag: Vorbis comments (FLAC/OGG)
// expose lowercase keys; MP3 keeps the values in ID3v2 TXXX user-text frames.
package main

import (
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/dhowden/tag"
)

// readReplayGain returns a track's ReplayGain (dB) and peak (linear, 0 if absent),
// and whether a gain value was found.
func readReplayGain(path string) (gainDB, peak float64, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	m, err := tag.ReadFrom(f)
	if err != nil {
		return 0, 0, false
	}
	raw := m.Raw()

	gainStr := vorbisOrTXXX(raw, "replaygain_track_gain", "REPLAYGAIN_TRACK_GAIN")
	peakStr := vorbisOrTXXX(raw, "replaygain_track_peak", "REPLAYGAIN_TRACK_PEAK")
	gainDB, ok = parseGainDB(gainStr)
	if !ok {
		return 0, 0, false
	}
	if p, err := strconv.ParseFloat(strings.TrimSpace(peakStr), 64); err == nil {
		peak = p
	}
	return gainDB, peak, true
}

// vorbisOrTXXX finds a ReplayGain value by its Vorbis-comment key (lowercased by
// dhowden) or, for MP3, by scanning ID3v2 TXXX frames for the named description.
func vorbisOrTXXX(raw map[string]interface{}, vorbisKey, txxxDesc string) string {
	if s, ok := raw[vorbisKey].(string); ok {
		return s
	}
	for k, v := range raw {
		if !strings.HasPrefix(k, "TXXX") {
			continue
		}
		var desc, text string
		switch c := v.(type) {
		case tag.Comm:
			desc, text = c.Description, c.Text
		case *tag.Comm:
			desc, text = c.Description, c.Text
		}
		if strings.EqualFold(desc, txxxDesc) {
			return text
		}
	}
	return ""
}

// parseGainDB parses a ReplayGain value such as "-6.48 dB" into a float.
func parseGainDB(s string) (float64, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimSpace(strings.TrimSuffix(s, "db"))
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// replayGainOffset returns the additive value for effects.Volume (Base 2) that
// applies a track's ReplayGain. factor = 10^(dB/20); when a peak is known the
// factor is capped so peak*factor <= 1 (no clipping). The result is log2(factor),
// which adds onto the user's gain in the volume chain. Returns 0 when no gain.
func replayGainOffset(path string) float64 {
	db, peak, ok := readReplayGain(path)
	if !ok {
		return 0
	}
	factor := math.Pow(10, db/20)
	if peak > 0 && factor*peak > 1 {
		factor = 1 / peak // clip protection
	}
	if factor <= 0 {
		return 0
	}
	return math.Log2(factor)
}
