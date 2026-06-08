// Package main - player.go is the audio playback engine, built on gopxl/beep
// with the oto speaker backend. It decodes MP3/FLAC/WAV/OGG, plays a queue in
// sequence, and - crucially for the user's "number of plays" tracking -
// increments a track's play count when it finishes playing naturally.
//
// A *skip* (Next/Prev) does NOT count as a play; only a track that plays to its
// end is counted. That matches the intent: a play means you listened to it.
package main

import (
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/effects"
	"github.com/gopxl/beep/v2/flac"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/speaker"
	"github.com/gopxl/beep/v2/vorbis"
	"github.com/gopxl/beep/v2/wav"
)

// playerSampleRate is the speaker's fixed output rate; streams at other rates
// are resampled to it. 44.1kHz suits typical music.
const playerSampleRate beep.SampleRate = 44100

// Player owns the speaker and the current play queue.
type Player struct {
	db *DB

	mu        sync.Mutex
	queue     []Track
	index     int // -1 when nothing is loaded
	streamer  beep.StreamSeekCloser
	format    beep.Format     // decoder format of the current track (for seeking)
	ctrl      *beep.Ctrl      // wraps the stream so we can pause/resume
	volume    *effects.Volume // wraps the chain so we can adjust gain
	gain      float64         // 0..1 linear volume, persists across tracks
	playing   bool
	inited    bool
	currentID int64   // DB id of the loaded track (0 if none)
	counted   bool    // whether the current track's play has been credited
	threshold float64 // fraction (0..1] of a track that must play to count as a play

	// OnChange is invoked whenever the current track or play state changes.
	// OnPlayCounted is invoked (with the track id) when a play is credited.
	// Both run off the UI goroutine, so Fyne implementations must wrap their
	// bodies in fyne.Do.
	OnChange      func()
	OnPlayCounted func(trackID int64)
}

func NewPlayer(db *DB) *Player {
	// Default threshold is 1.0 (count only at natural end); the UI overrides it
	// from the saved preference at startup via SetCountThreshold.
	return &Player{db: db, index: -1, gain: 1.0, threshold: 1.0}
}

// SetCountThreshold sets the fraction (0..1] of a track that must play before it
// counts as a play. 1.0 means only a track that plays to its natural end counts.
func (p *Player) SetCountThreshold(fraction float64) {
	if fraction <= 0 || fraction > 1 {
		fraction = 1.0
	}
	p.mu.Lock()
	p.threshold = fraction
	p.mu.Unlock()
}

// gainToVolume converts a 0..1 linear gain to the exponential Volume value used
// by effects.Volume (Base 2). g==1 -> 0 (no change); quieter -> negative.
func gainToVolume(g float64) float64 {
	if g <= 0 {
		return 0 // Silent flag handles muting
	}
	return math.Log2(g)
}

func (p *Player) ensureInit() error {
	if p.inited {
		return nil
	}
	if err := speaker.Init(playerSampleRate, playerSampleRate.N(time.Second/10)); err != nil {
		return err
	}
	p.inited = true
	return nil
}

// decodeFile opens and decodes a file by extension. The returned
// StreamSeekCloser owns the file handle; closing it releases the file.
func decodeFile(path string) (beep.StreamSeekCloser, beep.Format, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, beep.Format{}, err
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		return mp3.Decode(f)
	case ".flac":
		return flac.Decode(f)
	case ".wav":
		return wav.Decode(f)
	case ".ogg":
		return vorbis.Decode(f)
	}
	f.Close()
	return nil, beep.Format{}, fmt.Errorf("unsupported format: %s", path)
}

// PlayQueue replaces the queue and starts playing at index start.
func (p *Player) PlayQueue(tracks []Track, start int) {
	p.mu.Lock()
	p.queue = tracks
	p.index = start
	p.playLocked()
	p.mu.Unlock()
	p.fireChange() // fire AFTER unlocking - see fireChange's contract
}

// playLocked starts playback of queue[index]. Caller must hold p.mu AND must
// call p.fireChange() itself after releasing the lock (playLocked never fires
// the callback, to keep callbacks off the locked goroutine - see fireChange).
func (p *Player) playLocked() {
	if p.index < 0 || p.index >= len(p.queue) {
		return
	}
	if err := p.ensureInit(); err != nil {
		log.Printf("player: speaker init failed: %v", err)
		return
	}

	// Tear down whatever was playing.
	speaker.Clear()
	if p.streamer != nil {
		p.streamer.Close()
		p.streamer = nil
	}

	tr := p.queue[p.index]
	p.currentID = tr.ID
	p.counted = false // fresh track: not yet credited as a play
	s, format, err := decodeFile(tr.AbsPath())
	if err != nil {
		log.Printf("player: decode %q: %v", tr.AbsPath(), err)
		// Skip a bad file rather than stalling the queue.
		if p.index+1 < len(p.queue) {
			p.index++
			p.playLocked()
		}
		return
	}
	p.streamer = s
	p.format = format

	// Chain: decoder -> Ctrl (pause) -> resample (if needed) -> Volume (gain).
	p.ctrl = &beep.Ctrl{Streamer: s}
	var stream beep.Streamer = p.ctrl
	if format.SampleRate != playerSampleRate {
		stream = beep.Resample(4, format.SampleRate, playerSampleRate, p.ctrl)
	}
	p.volume = &effects.Volume{
		Streamer: stream,
		Base:     2,
		Volume:   gainToVolume(p.gain),
		Silent:   p.gain <= 0,
	}

	id := tr.ID
	// When the track drains naturally, the Callback fires on the speaker's
	// goroutine; we hand off to a fresh goroutine to avoid deadlocking on the
	// speaker mutex when advancing the queue.
	speaker.Play(beep.Seq(p.volume, beep.Callback(func() {
		go p.trackFinished(id)
	})))
	p.playing = true
	// NB: no fireChange here - the caller fires it after unlocking.
}

// trackFinished credits the play (if a percentage threshold hasn't already) and
// advances to the next queued track.
func (p *Player) trackFinished(id int64) {
	p.mu.Lock()
	// Credit at natural end if not already counted - covers the "end of track"
	// threshold and tracks too short for a 500ms tick to catch the crossing.
	credited, creditedID := p.creditPlayLocked()
	if p.index+1 < len(p.queue) {
		p.index++
		p.playLocked()
	} else {
		p.playing = false // reached the end of the queue
	}
	p.mu.Unlock()
	// Fire callbacks AFTER unlocking (see fireChange's contract).
	if credited {
		p.fireCounted(creditedID)
	}
	p.fireChange()
}

// creditPlayLocked records a play for the current track exactly once. The caller
// must hold p.mu. It only touches DB + state; the OnPlayCounted callback is fired
// by the caller AFTER unlocking (returns whether/which id was credited).
func (p *Player) creditPlayLocked() (credited bool, id int64) {
	if p.counted || p.currentID == 0 {
		return false, 0
	}
	p.counted = true
	id = p.currentID
	if err := p.db.IncrementPlayCount(id); err != nil {
		log.Printf("player: increment play count: %v", err)
	}
	return true, id
}

// MaybeCountPlay credits the current track once it has played past the configured
// threshold. Called periodically from the UI progress ticker (so it runs on the
// UI goroutine). No-op once the track is already counted or at the 100% setting
// (that case is credited at natural end in trackFinished).
func (p *Player) MaybeCountPlay() {
	p.mu.Lock()
	if p.streamer == nil || p.counted || p.threshold >= 1.0 {
		p.mu.Unlock()
		return
	}
	speaker.Lock()
	pos := p.streamer.Position()
	length := p.streamer.Len()
	speaker.Unlock()
	var credited bool
	var id int64
	if length > 0 && float64(pos)/float64(length) >= p.threshold {
		credited, id = p.creditPlayLocked()
	}
	p.mu.Unlock()
	// Fire callbacks AFTER unlocking (see fireChange's contract).
	if credited {
		p.fireCounted(id)
		p.fireChange() // refresh now-playing (stars may have changed)
	}
}

// Pause pauses playback if a track is playing (idempotent - safe to call when
// already paused or stopped). Used by the "hide all windows" boss key.
func (p *Player) Pause() {
	p.mu.Lock()
	if p.ctrl == nil {
		p.mu.Unlock()
		return
	}
	speaker.Lock()
	p.ctrl.Paused = true
	speaker.Unlock()
	p.playing = false
	p.mu.Unlock()
	p.fireChange()
}

// TogglePause pauses or resumes the current track.
func (p *Player) TogglePause() {
	p.mu.Lock()
	if p.ctrl == nil {
		p.mu.Unlock()
		return
	}
	speaker.Lock()
	p.ctrl.Paused = !p.ctrl.Paused
	paused := p.ctrl.Paused
	speaker.Unlock()
	p.playing = !paused
	p.mu.Unlock()
	p.fireChange()
}

// Next skips forward without counting the current track as played.
func (p *Player) Next() {
	p.mu.Lock()
	changed := false
	if p.index+1 < len(p.queue) {
		p.index++
		p.playLocked()
		changed = true
	}
	p.mu.Unlock()
	if changed {
		p.fireChange()
	}
}

// Prev skips back without counting the current track as played.
func (p *Player) Prev() {
	p.mu.Lock()
	changed := false
	if p.index > 0 {
		p.index--
		p.playLocked()
		changed = true
	}
	p.mu.Unlock()
	if changed {
		p.fireChange()
	}
}

// Stop halts playback and releases the current stream.
func (p *Player) Stop() {
	p.mu.Lock()
	speaker.Clear()
	if p.streamer != nil {
		p.streamer.Close()
		p.streamer = nil
	}
	p.ctrl = nil
	p.volume = nil
	p.format = beep.Format{}
	p.currentID = 0
	p.counted = false
	p.playing = false
	p.mu.Unlock()
	p.fireChange()
}

// SetVolume sets the playback gain (0..1) and applies it live. The level
// persists and is re-applied to subsequently played tracks.
func (p *Player) SetVolume(level float64) {
	if level < 0 {
		level = 0
	} else if level > 1 {
		level = 1
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gain = level
	if p.volume != nil {
		speaker.Lock()
		p.volume.Silent = level <= 0
		p.volume.Volume = gainToVolume(level)
		speaker.Unlock()
	}
}

// Volume returns the current gain (0..1).
func (p *Player) Volume() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.gain
}

// Seek jumps to a fraction (0..1) of the current track.
func (p *Player) Seek(fraction float64) {
	if fraction < 0 {
		fraction = 0
	} else if fraction > 1 {
		fraction = 1
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.streamer == nil {
		return
	}
	total := p.streamer.Len()
	pos := int(fraction * float64(total))
	if pos >= total {
		pos = total - 1
	}
	if pos < 0 {
		pos = 0
	}
	speaker.Lock()
	if err := p.streamer.Seek(pos); err != nil {
		log.Printf("player: seek: %v", err)
	}
	speaker.Unlock()
}

// Progress returns the current and total position of the loaded track.
func (p *Player) Progress() (pos, total time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.streamer == nil || p.format.SampleRate == 0 {
		return 0, 0
	}
	speaker.Lock()
	cur := p.streamer.Position()
	length := p.streamer.Len()
	speaker.Unlock()
	sr := float64(p.format.SampleRate)
	pos = time.Duration(float64(cur) / sr * float64(time.Second))
	total = time.Duration(float64(length) / sr * float64(time.Second))
	return pos, total
}

// Current returns the track now loaded and whether one is loaded.
func (p *Player) Current() (Track, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.index < 0 || p.index >= len(p.queue) {
		return Track{}, false
	}
	return p.queue[p.index], true
}

// IsPlaying reports whether audio is actively playing (not paused/stopped).
func (p *Player) IsPlaying() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.playing
}

// fireChange / fireCounted invoke the UI callbacks. CONTRACT: never call these
// while holding p.mu. The callbacks marshal onto the UI thread with fyne.Do,
// which - on the main goroutine during a window/menu callback - runs INLINE, and
// the callback (e.g. refreshNowPlaying) calls back into Player methods that lock
// p.mu. Firing under the lock therefore self-deadlocks the main thread (the
// Windows quit hang). Always unlock first, then fire.
func (p *Player) fireChange() {
	if p.OnChange != nil {
		p.OnChange()
	}
}

func (p *Player) fireCounted(id int64) {
	if p.OnPlayCounted != nil {
		p.OnPlayCounted(id)
	}
}
