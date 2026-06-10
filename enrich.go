// Package main - enrich.go fills in track metadata that the tag reader can't
// provide - currently playback duration - in the background, after launch and
// after each scan. dhowden/tag doesn't expose duration, so it's derived by
// decoding each file's length once and caching it in the catalog. The pass is
// throttled and stoppable so it never competes with playback or a busy UI.
package main

import (
	"log"
	"time"

	"fyne.io/fyne/v2"
)

// computeDuration returns a file's playback length in whole seconds by reading
// its decoded sample length (no audio is played). Works for every format the
// player decodes.
func computeDuration(path string) (int, error) {
	s, format, err := decodeFile(path)
	if err != nil {
		return 0, err
	}
	defer s.Close()
	n := s.Len()
	if n <= 0 || format.SampleRate == 0 {
		return 0, nil // unknown length; leave it for a later pass
	}
	return int(float64(n)/float64(format.SampleRate) + 0.5), nil
}

// startDurationEnricher computes and stores any missing track durations in the
// background. It's idempotent (guarded by u.enriching) so the startup call and a
// post-scan call can't run two passes at once, and it stops promptly on quit via
// the shared tickerDone channel.
func (u *ui) startDurationEnricher() {
	if !u.enriching.CompareAndSwap(false, true) {
		return // a pass is already running
	}
	go func() {
		defer u.enriching.Store(false)
		tracks, err := u.db.TracksMissingDuration()
		if err != nil {
			log.Printf("duration enricher: query: %v", err)
			return
		}
		var done int
		for _, tr := range tracks {
			select {
			case <-u.tickerDone:
				return // quitting - no more fyne.Do after teardown
			default:
			}
			secs, err := computeDuration(tr.AbsPath())
			if err != nil || secs <= 0 {
				continue
			}
			if err := u.db.SetDuration(tr.ID, secs); err != nil {
				continue
			}
			id, d := tr.ID, secs
			fyne.Do(func() { u.applyDuration(id, d) })
			done++
			// Refresh the visible table periodically so durations appear as they
			// fill in, without a full refresh per track.
			if done%25 == 0 {
				fyne.Do(u.table.Refresh)
			}
			time.Sleep(15 * time.Millisecond) // keep CPU/disk pressure low
		}
		if done > 0 {
			fyne.Do(u.table.Refresh)
		}
	}()
}

// applyDuration patches the in-memory track's duration. UI goroutine only; the
// enricher batches the table refresh, so this just updates the model.
func (u *ui) applyDuration(id int64, secs int) {
	for i := range u.tracks {
		if u.tracks[i].ID == id {
			u.tracks[i].Duration = secs
			return
		}
	}
}
