// Package main - rgscan.go measures track loudness and computes ReplayGain values
// (the analysis+write side, complementing the read+playback side in replaygain.go).
// It implements ITU-R BS.1770 / ReplayGain 2.0: K-weighting filter, 400 ms gated
// blocks, integrated loudness, gain = -18 LUFS - integrated. Audio is decoded via the
// player's decodeFile and resampled to 48 kHz so the canonical 48 kHz K-weighting
// coefficients apply without per-rate maths. Album gain pools the block measurements
// of every track in an album. The scan UI (scanReplayGainOf) reuses the batch
// progress/cancel pattern.
package main

import (
	"fmt"
	"log"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/gopxl/beep/v2"

	"mediaplayer/internal/i18n"
)

const (
	rgRefLUFS                     = -18.0 // ReplayGain 2.0 reference loudness
	rgAnalyzeRate beep.SampleRate = 48000 // K-weighting coefficients are for 48 kHz
)

// biquad is a Direct-Form-I second-order IIR section with its own state.
type biquad struct {
	b0, b1, b2, a1, a2 float64
	x1, x2, y1, y2     float64
}

func (f *biquad) process(x float64) float64 {
	y := f.b0*x + f.b1*f.x1 + f.b2*f.x2 - f.a1*f.y1 - f.a2*f.y2
	f.x2, f.x1 = f.x1, x
	f.y2, f.y1 = f.y1, y
	return y
}

// kWeighting returns the two BS.1770 K-weighting stages for 48 kHz: a high-shelf
// "pre-filter" and an RLB high-pass.
func kWeighting() (stage1, stage2 biquad) {
	stage1 = biquad{b0: 1.53512485958697, b1: -2.69169618940638, b2: 1.19839281085285,
		a1: -1.69065929318241, a2: 0.73248077421585}
	stage2 = biquad{b0: 1.0, b1: -2.0, b2: 1.0,
		a1: -1.99004745483398, a2: 0.99007225036621}
	return
}

// peakStreamer taps a stream to record the maximum absolute sample (linear sample
// peak) as samples pass through - placed before any resampler so the peak reflects
// the original audio.
type peakStreamer struct {
	s    beep.Streamer
	peak float64
}

func (p *peakStreamer) Stream(buf [][2]float64) (int, bool) {
	n, ok := p.s.Stream(buf)
	for i := 0; i < n; i++ {
		if a := math.Abs(buf[i][0]); a > p.peak {
			p.peak = a
		}
		if a := math.Abs(buf[i][1]); a > p.peak {
			p.peak = a
		}
	}
	return n, ok
}

func (p *peakStreamer) Err() error { return nil }

// analyzeLoudness decodes a file and returns its sample peak (linear) plus the
// channel-weighted mean-square value of each 400 ms block (100 ms hop, 75% overlap),
// pre-gating, so callers can compute track or pooled album loudness.
func analyzeLoudness(path string) (peak float64, blocks []float64, err error) {
	s, format, err := decodeFile(path)
	if err != nil {
		return 0, nil, err
	}
	defer s.Close()

	mono := format.NumChannels == 1
	pk := &peakStreamer{s: s}
	var src beep.Streamer = pk
	if format.SampleRate != rgAnalyzeRate {
		src = beep.Resample(4, format.SampleRate, rgAnalyzeRate, pk)
	}

	l1, l2 := kWeighting()
	r1, r2 := kWeighting()

	hop := rgAnalyzeRate.N(100 * time.Millisecond) // 4800 samples
	blockSamples := float64(hop * 4)               // 400 ms

	var hopL, hopR float64
	var hopCount int
	var ring [4][2]float64 // last 4 hops' sum-of-squares per channel
	var ringIdx, ringFilled int

	buf := make([][2]float64, 4096)
	for {
		n, ok := src.Stream(buf)
		for i := 0; i < n; i++ {
			yl := l2.process(l1.process(buf[i][0]))
			hopL += yl * yl
			if !mono {
				yr := r2.process(r1.process(buf[i][1]))
				hopR += yr * yr
			}
			hopCount++
			if hopCount == hop {
				ring[ringIdx] = [2]float64{hopL, hopR}
				ringIdx = (ringIdx + 1) % 4
				if ringFilled < 4 {
					ringFilled++
				}
				if ringFilled == 4 {
					var sl, sr float64
					for k := 0; k < 4; k++ {
						sl += ring[k][0]
						sr += ring[k][1]
					}
					z := sl / blockSamples
					if !mono {
						z += sr / blockSamples
					}
					blocks = append(blocks, z)
				}
				hopL, hopR, hopCount = 0, 0, 0
			}
		}
		if !ok {
			break
		}
	}
	return pk.peak, blocks, nil
}

// blockLoudness converts a block's channel-weighted mean square to LUFS.
func blockLoudness(z float64) float64 {
	if z <= 0 {
		return math.Inf(-1)
	}
	return -0.691 + 10*math.Log10(z)
}

// integratedLUFS applies the BS.1770 two-stage gating (absolute -70 LUFS, then
// relative -10 LU below the ungated mean) and returns the integrated loudness. ok is
// false when there's no measurable (non-silent) content.
func integratedLUFS(blocks []float64) (lufs float64, ok bool) {
	mean := func(xs []float64) float64 {
		var s float64
		for _, x := range xs {
			s += x
		}
		return s / float64(len(xs))
	}

	var absGated []float64
	for _, z := range blocks {
		if blockLoudness(z) >= -70 {
			absGated = append(absGated, z)
		}
	}
	if len(absGated) == 0 {
		return 0, false
	}
	relThresh := blockLoudness(mean(absGated)) - 10
	var kept []float64
	for _, z := range absGated {
		if blockLoudness(z) >= relThresh {
			kept = append(kept, z)
		}
	}
	if len(kept) == 0 {
		return 0, false
	}
	return blockLoudness(mean(kept)), true
}

// gainFromBlocks returns the ReplayGain (dB) for a set of block measurements: the
// reference loudness minus the integrated loudness. Returns 0 for silence.
func gainFromBlocks(blocks []float64) float64 {
	lufs, ok := integratedLUFS(blocks)
	if !ok {
		return 0
	}
	return rgRefLUFS - lufs
}

// albumKey groups tracks for album-gain pooling: album artist (or artist) + album,
// case-folded. An empty album means the track is its own "album".
func albumKey(t Track) string {
	artist := t.AlbumArtist
	if strings.TrimSpace(artist) == "" {
		artist = t.Artist
	}
	if strings.TrimSpace(t.Album) == "" {
		return "\x00track\x00" + t.RelPath // unique per track
	}
	return strings.ToLower(strings.TrimSpace(artist)) + "\x00" + strings.ToLower(strings.TrimSpace(t.Album))
}

// scanReplayGainOf analyzes the given tracks, computes track and album gain, and
// writes the ReplayGain tags, all on a background goroutine with a progress/cancel
// dialog (mirrors applyTagPatch). OGG/WAV and unreadable files are skipped.
func (u *ui) scanReplayGainOf(tracks []Track) {
	if len(tracks) == 0 {
		return
	}
	// Stop playback if a target is the currently-playing track (open handle blocks the
	// rewrite on Windows).
	if cur, ok := u.player.Current(); ok {
		for _, t := range tracks {
			if t.ID == cur.ID {
				u.player.Stop()
				u.status.SetText(i18n.T("status.stopped_scan_rg"))
				break
			}
		}
	}

	total := len(tracks)
	statusLbl := widget.NewLabel(i18n.TC("rgscan.scanning_n", map[string]string{"n": fmt.Sprintf("%d", total)}))
	statusLbl.Truncation = fyne.TextTruncateEllipsis
	prog := widget.NewProgressBar()
	prog.Max = float64(total)
	var canceled atomic.Bool
	cancelBtn := widget.NewButton(i18n.T("common.cancel"), func() { canceled.Store(true) })
	cancelBtn.Importance = widget.DangerImportance
	d := dialog.NewCustomWithoutButtons(i18n.T("rgscan.scanning_title"), container.NewVBox(
		statusLbl, prog, container.NewCenter(cancelBtn),
	), u.win)
	d.Resize(fyne.NewSize(440, 150))
	d.Show()

	go func() {
		// Pass 1: analyze each writable track, grouping block measurements by album.
		type measured struct {
			t      Track
			peak   float64
			blocks []float64
		}
		var items []measured
		albumBlocks := map[string][]float64{}
		albumPeak := map[string]float64{}
		var skipped, failed int

		for i, t := range tracks {
			if canceled.Load() {
				break
			}
			n, name := i+1, filepath.Base(t.RelPath)
			fyne.Do(func() {
				statusLbl.SetText(i18n.TC("rgscan.scanning_progress", map[string]string{
					"n": fmt.Sprintf("%d", n), "total": fmt.Sprintf("%d", total), "name": name,
				}))
				prog.SetValue(float64(n))
			})
			if !tagsWritable(t.AbsPath()) {
				skipped++
				continue
			}
			peak, blocks, err := analyzeLoudness(t.AbsPath())
			if err != nil {
				log.Printf("replaygain scan: %q: %v", t.AbsPath(), err)
				failed++
				continue
			}
			items = append(items, measured{t: t, peak: peak, blocks: blocks})
			k := albumKey(t)
			albumBlocks[k] = append(albumBlocks[k], blocks...)
			if peak > albumPeak[k] {
				albumPeak[k] = peak
			}
		}

		// Album gains, computed once per group from the pooled blocks.
		albumGain := map[string]float64{}
		for k, b := range albumBlocks {
			albumGain[k] = gainFromBlocks(b)
		}

		// Pass 2: write the tags.
		var written int
		for _, m := range items {
			if canceled.Load() {
				break
			}
			k := albumKey(m.t)
			err := writeReplayGainTags(m.t.AbsPath(),
				gainFromBlocks(m.blocks), m.peak, albumGain[k], albumPeak[k])
			if err != nil {
				log.Printf("replaygain write: %q: %v", m.t.AbsPath(), err)
				failed++
				continue
			}
			written++
		}

		wasCanceled := canceled.Load()
		fyne.Do(func() {
			d.Hide()
			summary := i18n.TC("rgscan.wrote", map[string]string{"n": fmt.Sprintf("%d", written)})
			if skipped > 0 {
				summary += i18n.TC("common.skipped_unwritable", map[string]string{"n": fmt.Sprintf("%d", skipped)})
			}
			if failed > 0 {
				summary += i18n.TC("common.failed_log", map[string]string{"n": fmt.Sprintf("%d", failed)})
			}
			title := i18n.T("rgscan.complete")
			if wasCanceled {
				title = i18n.T("rgscan.cancelled")
				summary = i18n.T("common.cancelled_prefix") + summary
			}
			u.status.SetText(summary)
			dialog.ShowInformation(title, summary, u.win)
		})
	}()
}

// scanReplayGainSelected is the Library-menu entry point: scan the marked tracks.
func (u *ui) scanReplayGainSelected() {
	if len(u.marked) == 0 {
		dialog.ShowInformation(i18n.T("rgscan.scan_title"), i18n.T("queue.none_marked"), u.win)
		return
	}
	u.scanReplayGainOf(u.markedTracksForScan())
}

// markedTracksForScan returns the marked tracks (for the batch entry points). It
// reads from the current view where possible and falls back to a catalog lookup so it
// covers marked tracks scrolled out of view.
func (u *ui) markedTracksForScan() []Track {
	out := make([]Track, 0, len(u.marked))
	inView := map[int64]Track{}
	for _, t := range u.tracks {
		inView[t.ID] = t
	}
	for id, e := range u.marked {
		if t, ok := inView[id]; ok {
			out = append(out, t)
			continue
		}
		// Not in the current view: synthesize the minimum Track the scan needs.
		out = append(out, Track{ID: id, FolderRoot: filepath.Dir(e.path), RelPath: filepath.Base(e.path), Artist: e.artist, Album: e.album})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RelPath < out[j].RelPath })
	return out
}
