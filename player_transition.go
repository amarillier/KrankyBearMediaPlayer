// Package main - player_transition.go adds opt-in gapless and crossfade track
// transitions on top of the default "gap" playback. EXPERIMENTAL.
//
// Approach (deliberately avoids rewriting the play loop): a poll goroutine
// watches how much of the current track remains; when it drops below a lead time
// it starts the NEXT track early via speaker.Play, which MIXES into the same
// output rather than clearing it. Gapless uses a tiny lead so the tracks butt up;
// crossfade uses a longer lead and ramps the outgoing track down while ramping
// the incoming one up. A generation counter (player.go) makes the outgoing
// track's end-callback a no-op so it can't double-advance; superseded streams are
// tracked in p.draining so they're always closed.
//
// Lock order is unchanged from the rest of the player (p.mu, then the speaker
// mutex), so this adds no new deadlock path.
package main

import (
	"log"
	"time"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/effects"
	"github.com/gopxl/beep/v2/speaker"
)

// transitionMode selects how one track gives way to the next.
type transitionMode int

const (
	transGap       transitionMode = iota // default: stop, then start the next (a brief gap)
	transGapless                         // start the next so they butt up with ~no gap
	transCrossfade                       // overlap the two, fading out/in
)

const (
	gaplessLeadSecs = 0.18  // how early to start the next track for gapless
	crossfadeSecs   = 4.0   // overlap/fade length for crossfade
	silentVolumeL2  = -12.0 // ~-72 dB in the Base-2 volume; the "off" end of a fade
)

// SetTransitionMode sets the track-transition behaviour. Safe to call any time.
func (p *Player) SetTransitionMode(m transitionMode) {
	p.mu.Lock()
	p.xfade = m
	p.mu.Unlock()
}

// startTransitionPoll launches the once-only watcher that triggers gapless /
// crossfade transitions. Started from ensureInit (the speaker is up by then); it
// runs for the app's life and is a no-op in gap mode or when paused/stopped.
func (p *Player) startTransitionPoll() {
	p.poller.Do(func() {
		go func() {
			t := time.NewTicker(120 * time.Millisecond)
			defer t.Stop()
			for range t.C {
				p.mu.Lock()
				p.maybeStartTransitionLocked()
				p.mu.Unlock()
			}
		}()
	})
}

// maybeStartTransitionLocked starts the next track early when the current one is
// within the lead time of its end. Caller holds p.mu.
func (p *Player) maybeStartTransitionLocked() {
	if p.xfade == transGap || !p.playing || !p.armed || p.streamer == nil || p.ctrl == nil {
		return
	}
	speaker.Lock()
	paused := p.ctrl.Paused
	total := p.streamer.Len()
	pos := p.streamer.Position()
	speaker.Unlock()
	if paused || total <= 0 || p.format.SampleRate == 0 {
		return
	}
	sr := float64(p.format.SampleRate)
	totalSecs := float64(total) / sr
	remaining := float64(total-pos) / sr

	lead := gaplessLeadSecs
	if p.xfade == transCrossfade {
		lead = crossfadeSecs
	}
	// Skip very short tracks - pre-starting them would dominate or run away.
	if totalSecs < lead*2 {
		return
	}
	if remaining > lead {
		return
	}
	p.armed = false // trigger only once for this track
	p.startTransitionLocked()
}

// startTransitionLocked decodes the next track and hands over to it while the
// current track keeps playing (mixed). Caller holds p.mu.
func (p *Player) startTransitionLocked() {
	np, nextIndex, ok := p.nextOrderPosLocked()
	if !ok {
		return // end of queue (no repeat): let the natural end handle it
	}
	tr := p.queue[nextIndex]
	s, format, err := decodeFile(tr.AbsPath())
	if err != nil {
		log.Printf("player: transition decode %q: %v", tr.AbsPath(), err)
		return // natural end will advance/skip as usual
	}

	rg := 0.0
	if p.replayGain {
		rg = replayGainOffset(tr.AbsPath(), p.rgAlbum)
	}
	target := gainToVolume(p.gain) + rg
	crossfade := p.xfade == transCrossfade
	startVol := target
	if crossfade {
		startVol = silentVolumeL2 // ramp up from near-silence
	}

	ctrl := &beep.Ctrl{Streamer: s}
	var stream beep.Streamer = ctrl
	if format.SampleRate != playerSampleRate {
		stream = beep.Resample(4, format.SampleRate, playerSampleRate, ctrl)
	}
	eq := newEQStreamer(stream, p.eqGains, p.eqEnabled)
	stream = eq
	bal := newBalanceStreamer(stream, p.balance)
	stream = bal
	vol := &effects.Volume{Streamer: stream, Base: 2, Volume: startVol, Silent: p.gain <= 0}

	credited, creditedID := p.creditPlayLocked() // the outgoing track is done

	oldVol, oldGain := p.volume, p.gain
	if p.streamer != nil {
		p.draining = append(p.draining, p.streamer) // keep its handle until it drains/clears
	}

	// Hand over: the next track becomes current; its end-callback gets a fresh gen
	// (in beginStreamLocked), so the outgoing track's callback is now stale.
	p.index, p.pos = nextIndex, np
	p.streamer, p.format, p.ctrl, p.volume = s, format, ctrl, vol
	p.eqCur = eq
	p.balCur = bal
	p.currentID, p.counted, p.rgOffset = tr.ID, false, rg
	p.beginStreamLocked(tr) // Play (mixes - does NOT clear, so the old keeps draining)

	if crossfade && oldGain > 0 {
		go p.crossfadeRamp(oldVol, vol, gainToVolume(oldGain), target)
	}

	go func() {
		if credited {
			p.fireCounted(creditedID)
		}
		p.fireChange()
	}()
}

// nextOrderPosLocked returns the play-order position + queue index that would
// play next, honouring RepeatOne (same track) and RepeatAll (wrap). ok is false
// at the end of the queue without RepeatAll. Caller holds p.mu.
func (p *Player) nextOrderPosLocked() (pos, index int, ok bool) {
	n := len(p.order)
	if n == 0 {
		return 0, 0, false
	}
	if p.repeat == RepeatOne {
		return p.pos, p.index, true
	}
	np := p.pos + 1
	if np >= n {
		if p.repeat != RepeatAll {
			return 0, 0, false
		}
		np = 0
	}
	return np, p.order[np], true
}

// crossfadeRamp fades oldVol down to silence and newVol up to target over
// crossfadeSecs. Runs on its own goroutine; only touches the speaker mutex.
func (p *Player) crossfadeRamp(oldVol, newVol *effects.Volume, oldStart, newTarget float64) {
	const steps = 48
	step := time.Duration(crossfadeSecs*float64(time.Second)) / steps
	for i := 1; i <= steps; i++ {
		time.Sleep(step)
		frac := float64(i) / steps
		speaker.Lock()
		oldVol.Silent = false
		oldVol.Volume = lerp(oldStart, silentVolumeL2, frac)
		newVol.Silent = false
		newVol.Volume = lerp(silentVolumeL2, newTarget, frac)
		speaker.Unlock()
	}
	speaker.Lock()
	oldVol.Silent = true
	newVol.Volume = newTarget
	speaker.Unlock()
}

// dropDrainingLocked removes s from the draining set (it has finished). Caller
// holds p.mu.
func (p *Player) dropDrainingLocked(s beep.StreamSeekCloser) {
	for i, d := range p.draining {
		if d == s {
			p.draining = append(p.draining[:i], p.draining[i+1:]...)
			return
		}
	}
}

// closeDrainingLocked closes and forgets every overlap stream. Called from the
// teardown paths (playLocked / stopStreamLocked) after speaker.Clear, which
// discards those streams so their end-callbacks (which would otherwise close
// them) never fire. Caller holds p.mu.
func (p *Player) closeDrainingLocked() {
	for _, s := range p.draining {
		s.Close()
	}
	p.draining = nil
}

func lerp(a, b, t float64) float64 { return a + (b-a)*t }
