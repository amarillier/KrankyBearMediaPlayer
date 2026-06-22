// Package main - balance.go is the stereo balance (left/right) control: a streamer
// that scales the L and R channels by independent gains derived from a single
// balance position (-1 full left .. 0 center .. +1 full right). It sits in the
// playback chain (player.go) right after the equalizer and updates live like the
// EQ. It uses a linear balance law - the standard "balance" (not "pan") behaviour:
// at center both channels pass at unity; toward one side the OPPOSITE channel
// fades to silence while the near channel stays at unity. Center is a true bypass.
package main

import "github.com/gopxl/beep/v2"

// balanceGains converts a balance position (-1..+1) to per-channel linear gains.
// Center (0) is unity on both; panning right attenuates left and vice versa.
func balanceGains(pos float64) (l, r float64) {
	if pos < -1 {
		pos = -1
	} else if pos > 1 {
		pos = 1
	}
	l, r = 1.0, 1.0
	if pos > 0 {
		l = 1 - pos // toward right: fade the left channel out
	} else if pos < 0 {
		r = 1 + pos // toward left: fade the right channel out
	}
	return l, r
}

// balanceStreamer wraps a source stream and attenuates one channel to shift the
// stereo image left or right. Like eqStreamer, it owns no state beyond its gains,
// so a live change can't pop.
type balanceStreamer struct {
	src   beep.Streamer
	lGain float64
	rGain float64
}

// newBalanceStreamer builds a balance stage at the given position (-1..+1).
func newBalanceStreamer(src beep.Streamer, pos float64) *balanceStreamer {
	b := &balanceStreamer{src: src}
	b.setBalance(pos)
	return b
}

// setBalance recomputes the per-channel gains for a balance position (-1..+1).
func (b *balanceStreamer) setBalance(pos float64) {
	b.lGain, b.rGain = balanceGains(pos)
}

func (b *balanceStreamer) Stream(buf [][2]float64) (int, bool) {
	n, ok := b.src.Stream(buf)
	// Center is unity on both channels; skip the multiply entirely so the common
	// case adds no per-sample cost.
	if b.lGain != 1.0 || b.rGain != 1.0 {
		for i := 0; i < n; i++ {
			buf[i][0] *= b.lGain
			buf[i][1] *= b.rGain
		}
	}
	return n, ok
}

func (b *balanceStreamer) Err() error { return nil }
