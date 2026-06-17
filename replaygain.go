// Package main - replaygain.go reads ReplayGain track tags and converts them to a
// gain offset for the playback volume chain (player.go). ReplayGain stores, per
// track, how many dB to adjust playback so everything plays at a consistent
// loudness; an optional peak value lets us cap the gain so boosted (quiet) tracks
// don't clip. Reading is uniform via dhowden/tag: Vorbis comments (FLAC/OGG)
// expose lowercase keys; MP3 keeps the values in ID3v2 TXXX user-text frames.
package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bogem/id3v2/v2"
	"github.com/dhowden/tag"
	flacvorbis "github.com/go-flac/flacvorbis"
	flac "github.com/go-flac/go-flac"
)

// rgKeys are the ReplayGain comment/TXXX keys this app reads and writes.
var rgKeys = map[string]bool{
	"REPLAYGAIN_TRACK_GAIN": true, "REPLAYGAIN_TRACK_PEAK": true,
	"REPLAYGAIN_ALBUM_GAIN": true, "REPLAYGAIN_ALBUM_PEAK": true,
}

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

// readReplayGainMode reads a track's ReplayGain, preferring album values when
// preferAlbum is set (falling back to track, and vice-versa). Returns ok=false when
// no usable gain is tagged.
func readReplayGainMode(path string, preferAlbum bool) (gainDB, peak float64, ok bool) {
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

	tg, tgok := parseGainDB(vorbisOrTXXX(raw, "replaygain_track_gain", "REPLAYGAIN_TRACK_GAIN"))
	tp := parsePeak(vorbisOrTXXX(raw, "replaygain_track_peak", "REPLAYGAIN_TRACK_PEAK"))
	ag, agok := parseGainDB(vorbisOrTXXX(raw, "replaygain_album_gain", "REPLAYGAIN_ALBUM_GAIN"))
	ap := parsePeak(vorbisOrTXXX(raw, "replaygain_album_peak", "REPLAYGAIN_ALBUM_PEAK"))

	if preferAlbum && agok {
		return ag, ap, true
	}
	if tgok {
		return tg, tp, true
	}
	if agok { // no track gain but album is present
		return ag, ap, true
	}
	return 0, 0, false
}

// parsePeak parses a linear ReplayGain peak value (0 if absent/unparseable).
func parsePeak(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return v
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
func replayGainOffset(path string, preferAlbum bool) float64 {
	db, peak, ok := readReplayGainMode(path, preferAlbum)
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

// fmtRGGain / fmtRGPeak format ReplayGain values the conventional way
// ("-6.48 dB", "0.987654").
func fmtRGGain(db float64) string { return fmt.Sprintf("%.2f dB", db) }
func fmtRGPeak(p float64) string  { return fmt.Sprintf("%.6f", p) }

// writeReplayGainTags writes track + album ReplayGain into a file, preserving all
// other tags. MP3 (TXXX) and FLAC (Vorbis comments) only; OGG/WAV are unsupported.
func writeReplayGainTags(path string, trackGain, trackPeak, albumGain, albumPeak float64) error {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		return writeRGMP3(path, trackGain, trackPeak, albumGain, albumPeak)
	case ".flac":
		return writeRGFLAC(path, trackGain, trackPeak, albumGain, albumPeak)
	}
	return fmt.Errorf("ReplayGain write not supported for %s", filepath.Ext(path))
}

// writeRGMP3 replaces the ReplayGain TXXX frames, leaving every other frame intact.
func writeRGMP3(path string, tg, tp, ag, ap float64) error {
	t, err := id3v2.Open(path, id3v2.Options{Parse: true})
	if err != nil {
		return err
	}
	defer t.Close()
	enc := id3v2.EncodingUTF8

	// Preserve non-ReplayGain TXXX frames (DeleteFrames removes all TXXX at once).
	var keep []id3v2.UserDefinedTextFrame
	for _, fr := range t.GetFrames("TXXX") {
		if u, ok := fr.(id3v2.UserDefinedTextFrame); ok && !rgKeys[strings.ToUpper(u.Description)] {
			keep = append(keep, u)
		}
	}
	t.DeleteFrames("TXXX")
	for _, u := range keep {
		t.AddUserDefinedTextFrame(u)
	}
	add := func(desc, val string) {
		t.AddUserDefinedTextFrame(id3v2.UserDefinedTextFrame{Encoding: enc, Description: desc, Value: val})
	}
	add("REPLAYGAIN_TRACK_GAIN", fmtRGGain(tg))
	add("REPLAYGAIN_TRACK_PEAK", fmtRGPeak(tp))
	add("REPLAYGAIN_ALBUM_GAIN", fmtRGGain(ag))
	add("REPLAYGAIN_ALBUM_PEAK", fmtRGPeak(ap))
	return saveMP3UTF8(t) // bogem mangles UTF-16 frames; persist as UTF-8
}

// writeRGFLAC rebuilds the Vorbis comment block with the ReplayGain fields replaced,
// keeping every other comment and metadata block (incl. pictures).
func writeRGFLAC(path string, tg, tp, ag, ap float64) error {
	f, err := flac.ParseFile(path)
	if err != nil {
		return err
	}

	cmt := flacvorbis.New()
	for _, b := range f.Meta {
		if b.Type != flac.VorbisComment {
			continue
		}
		if existing, err := flacvorbis.ParseFromMetaDataBlock(*b); err == nil {
			cmt.Vendor = existing.Vendor
			for _, c := range existing.Comments {
				key := c
				if i := strings.IndexByte(c, '='); i >= 0 {
					key = c[:i]
				}
				if !rgKeys[strings.ToUpper(key)] {
					cmt.Comments = append(cmt.Comments, c)
				}
			}
		}
		break
	}
	_ = cmt.Add("REPLAYGAIN_TRACK_GAIN", fmtRGGain(tg))
	_ = cmt.Add("REPLAYGAIN_TRACK_PEAK", fmtRGPeak(tp))
	_ = cmt.Add("REPLAYGAIN_ALBUM_GAIN", fmtRGGain(ag))
	_ = cmt.Add("REPLAYGAIN_ALBUM_PEAK", fmtRGPeak(ap))
	cmtBlock := cmt.Marshal()

	kept := f.Meta[:0]
	for _, b := range f.Meta {
		if b.Type == flac.VorbisComment {
			continue
		}
		kept = append(kept, b)
	}
	f.Meta = append(kept, &cmtBlock)
	return f.Save(path)
}
