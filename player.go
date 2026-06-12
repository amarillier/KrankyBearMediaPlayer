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
	"math/rand"
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

// RepeatMode controls what happens when a track (or the whole queue) ends.
type RepeatMode int

const (
	RepeatOff RepeatMode = iota // stop at the end of the queue
	RepeatAll                   // wrap around to the start of the queue
	RepeatOne                   // replay the current track forever
)

// Player owns the speaker and the current play queue.
type Player struct {
	db *DB

	mu    sync.Mutex
	queue []Track
	index int // index into queue of the current track; -1 when nothing loaded
	// order is a permutation of queue indices giving the play order (identity when
	// not shuffled); pos is the position within order, so order[pos] == index.
	order      []int
	pos        int
	shuffle    bool
	repeat     RepeatMode
	streamer   beep.StreamSeekCloser
	draining   []beep.StreamSeekCloser // superseded streams still draining during a gapless/crossfade overlap
	format     beep.Format             // decoder format of the current track (for seeking)
	ctrl       *beep.Ctrl              // wraps the stream so we can pause/resume
	volume     *effects.Volume         // wraps the chain so we can adjust gain
	gain       float64                 // 0..1 linear volume, persists across tracks
	replayGain bool                    // apply per-track ReplayGain when available
	rgOffset   float64                 // current track's ReplayGain offset (log2 factor; 0 = none)
	// Track transition (player_transition.go): gapless / crossfade. gen rises each
	// time a new stream becomes current, so a superseded track's end-callback is
	// ignored. armed = the current track may still pre-trigger its transition.
	xfade     transitionMode
	gen       uint64
	armed     bool
	poller    sync.Once
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
	p.startTransitionPoll() // watches for gapless/crossfade transitions (no-op in gap mode)
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
	p.rebuildOrderLocked(start)
	p.playLocked()
	p.mu.Unlock()
	p.fireChange() // fire AFTER unlocking - see fireChange's contract
}

// rebuildOrderLocked recomputes the play order over the current queue, leaving
// the given queue index (the track to keep "current") at the front when shuffled
// and pointing pos at it either way. Caller must hold p.mu.
func (p *Player) rebuildOrderLocked(current int) {
	n := len(p.queue)
	p.order = make([]int, n)
	for i := range p.order {
		p.order[i] = i
	}
	if p.shuffle && n > 1 {
		rand.Shuffle(n, func(i, j int) { p.order[i], p.order[j] = p.order[j], p.order[i] })
		// Keep the current track playing: move it to the front of the order.
		if current >= 0 {
			for i, q := range p.order {
				if q == current {
					p.order[0], p.order[i] = p.order[i], p.order[0]
					break
				}
			}
		}
	}
	// Point pos at the current track's slot in the (possibly shuffled) order.
	p.pos = 0
	for i, q := range p.order {
		if q == current {
			p.pos = i
			break
		}
	}
}

// stepLocked moves pos by delta (+1 next, -1 prev) within the play order and
// starts the new track, honouring RepeatAll wrap-around. Returns false (and does
// nothing) when stepping off either end without RepeatAll. Manual skips override
// RepeatOne. Caller must hold p.mu.
func (p *Player) stepLocked(delta int) bool {
	n := len(p.order)
	if n == 0 {
		return false
	}
	np := p.pos + delta
	if np < 0 || np >= n {
		if p.repeat != RepeatAll {
			return false
		}
		np = (np%n + n) % n
	}
	p.pos = np
	p.index = p.order[p.pos]
	p.playLocked()
	return true
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

	// Tear down whatever was playing. Clear discards any overlap streams from the
	// mixer (so their end-callbacks won't fire) - close them here instead.
	speaker.Clear()
	if p.streamer != nil {
		p.streamer.Close()
		p.streamer = nil
	}
	p.closeDrainingLocked()

	tr := p.queue[p.index]
	p.currentID = tr.ID
	p.counted = false // fresh track: not yet credited as a play
	s, format, err := decodeFile(tr.AbsPath())
	if err != nil {
		log.Printf("player: decode %q: %v", tr.AbsPath(), err)
		// Skip a bad file rather than stalling the queue. Advance through the play
		// order (so shuffle is honoured) but never wrap, even under RepeatAll - an
		// all-bad queue must stop rather than loop forever.
		if p.pos+1 < len(p.order) {
			p.pos++
			p.index = p.order[p.pos]
			p.playLocked()
		}
		return
	}
	p.streamer = s
	p.format = format

	// ReplayGain (opt-in): fold the track's gain offset into the volume below.
	p.rgOffset = 0
	if p.replayGain {
		p.rgOffset = replayGainOffset(tr.AbsPath())
	}

	// Chain: decoder -> Ctrl (pause) -> resample (if needed) -> Volume (gain).
	p.ctrl = &beep.Ctrl{Streamer: s}
	var stream beep.Streamer = p.ctrl
	if format.SampleRate != playerSampleRate {
		stream = beep.Resample(4, format.SampleRate, playerSampleRate, p.ctrl)
	}
	p.volume = &effects.Volume{
		Streamer: stream,
		Base:     2,
		Volume:   gainToVolume(p.gain) + p.rgOffset,
		Silent:   p.gain <= 0,
	}

	p.beginStreamLocked(tr) // adds this stream to the speaker (replacing the old)
	// NB: no fireChange here - the caller fires it after unlocking.
}

// beginStreamLocked adds the already-set-up current stream (p.streamer/p.volume)
// to the speaker and wires its end callback under a fresh generation. Used by
// playLocked (gap mode) and the gapless/crossfade transition. Caller holds p.mu.
func (p *Player) beginStreamLocked(tr Track) {
	p.gen++
	gen := p.gen
	p.armed = true
	s := p.streamer
	id := tr.ID
	// When the track drains naturally the Callback fires on the speaker's
	// goroutine; hand off to a fresh goroutine to avoid deadlocking on the speaker
	// mutex when advancing the queue.
	speaker.Play(beep.Seq(p.volume, beep.Callback(func() {
		go p.trackEnded(id, gen, s)
	})))
	p.playing = true
}

// trackEnded runs when a stream drains. If its generation is stale (a gapless/
// crossfade transition already moved on), it just releases the streamer. Otherwise
// it credits the play and advances to the next queued track.
func (p *Player) trackEnded(id int64, gen uint64, s beep.StreamSeekCloser) {
	p.mu.Lock()
	if gen != p.gen {
		// Superseded by a transition; this old stream has finished draining.
		p.dropDrainingLocked(s)
		p.mu.Unlock()
		if s != nil {
			s.Close()
		}
		return
	}
	// Credit at natural end if not already counted - covers the "end of track"
	// threshold and tracks too short for a 500ms tick to catch the crossing.
	credited, creditedID := p.creditPlayLocked()
	if p.repeat == RepeatOne {
		p.playLocked() // loop the same track
	} else if !p.stepLocked(1) {
		p.playing = false // reached the end of the queue (no RepeatAll)
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
	changed := p.stepLocked(1)
	p.mu.Unlock()
	if changed {
		p.fireChange()
	}
}

// Prev skips back without counting the current track as played.
func (p *Player) Prev() {
	p.mu.Lock()
	changed := p.stepLocked(-1)
	p.mu.Unlock()
	if changed {
		p.fireChange()
	}
}

// SetShuffle turns shuffle on or off, rebuilding the play order while keeping the
// current track playing. Persisted by the UI; safe to call any time.
func (p *Player) SetShuffle(on bool) {
	p.mu.Lock()
	if p.shuffle != on {
		p.shuffle = on
		p.rebuildOrderLocked(p.index)
	}
	p.mu.Unlock()
}

// Shuffle reports whether shuffle is on.
func (p *Player) Shuffle() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.shuffle
}

// SetRepeat sets the repeat mode (off / all / one).
func (p *Player) SetRepeat(m RepeatMode) {
	p.mu.Lock()
	p.repeat = m
	p.mu.Unlock()
}

// Repeat returns the current repeat mode.
func (p *Player) Repeat() RepeatMode {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.repeat
}

// Enqueue appends tracks to the end of the play queue without interrupting the
// current track. The new tracks play after everything already queued. If the
// queue was empty, playback starts at the first appended track.
func (p *Player) Enqueue(tracks []Track) {
	if len(tracks) == 0 {
		return
	}
	p.mu.Lock()
	startEmpty := len(p.queue) == 0
	base := len(p.queue)
	p.queue = append(p.queue, tracks...)
	for i := range tracks {
		p.order = append(p.order, base+i)
	}
	if startEmpty {
		p.pos = 0
		p.index = p.order[0]
		p.playLocked()
	}
	p.mu.Unlock()
	p.fireChange()
}

// QueueView returns the queued tracks in play order, plus the order-position of
// the currently-playing track (-1 if none). Used by the Play Queue window.
func (p *Player) QueueView() (tracks []Track, current int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	tracks = make([]Track, len(p.order))
	for i, qi := range p.order {
		tracks[i] = p.queue[qi]
	}
	current = -1
	if p.index >= 0 && p.pos >= 0 && p.pos < len(p.order) && p.order[p.pos] == p.index {
		current = p.pos
	}
	return
}

// JumpTo starts playing the track at the given play-order position.
func (p *Player) JumpTo(orderPos int) {
	p.mu.Lock()
	changed := false
	if orderPos >= 0 && orderPos < len(p.order) {
		p.pos = orderPos
		p.index = p.order[p.pos]
		p.playLocked()
		changed = true
	}
	p.mu.Unlock()
	if changed {
		p.fireChange()
	}
}

// RemoveAt removes the track at the given play-order position. Removing the
// currently-playing track stops playback (the queue keeps its other entries).
func (p *Player) RemoveAt(orderPos int) {
	p.mu.Lock()
	if orderPos < 0 || orderPos >= len(p.order) {
		p.mu.Unlock()
		return
	}
	qi := p.order[orderPos]
	removingCurrent := qi == p.index
	// Drop the queue entry and renumber every order index that pointed past it.
	p.queue = append(p.queue[:qi], p.queue[qi+1:]...)
	newOrder := make([]int, 0, len(p.order)-1)
	for op, q := range p.order {
		if op == orderPos {
			continue
		}
		if q > qi {
			q--
		}
		newOrder = append(newOrder, q)
	}
	p.order = newOrder
	if orderPos < p.pos {
		p.pos--
	}
	switch {
	case removingCurrent:
		p.stopStreamLocked()
		p.index = -1
		if p.pos >= len(p.order) {
			p.pos = len(p.order) - 1 // clamp; -1 when the queue is now empty
		}
	case p.index > qi:
		p.index--
	}
	p.mu.Unlock()
	p.fireChange()
}

// MoveAt moves the queue entry at play-order position from to position to,
// reordering the upcoming play order. The currently-playing track keeps playing.
func (p *Player) MoveAt(from, to int) {
	p.mu.Lock()
	n := len(p.order)
	if from < 0 || from >= n || to < 0 || to >= n || from == to {
		p.mu.Unlock()
		return
	}
	q := p.order[from]
	p.order = append(p.order[:from], p.order[from+1:]...)
	p.order = append(p.order[:to], append([]int{q}, p.order[to:]...)...)
	// pos follows the currently-loaded track to its new slot.
	for i, qi := range p.order {
		if qi == p.index {
			p.pos = i
			break
		}
	}
	p.mu.Unlock()
	p.fireChange()
}

// ClearQueue stops playback and empties the queue.
func (p *Player) ClearQueue() {
	p.mu.Lock()
	p.stopStreamLocked()
	p.queue = nil
	p.order = nil
	p.pos = 0
	p.index = -1
	p.mu.Unlock()
	p.fireChange()
}

// stopStreamLocked tears down the current stream and clears play state without
// touching the queue. Caller must hold p.mu.
func (p *Player) stopStreamLocked() {
	speaker.Clear()
	if p.streamer != nil {
		p.streamer.Close()
		p.streamer = nil
	}
	p.closeDrainingLocked()
	p.ctrl = nil
	p.volume = nil
	p.format = beep.Format{}
	p.currentID = 0
	p.counted = false
	p.playing = false
	p.gen++ // invalidate any in-flight end-callbacks
}

// Stop halts playback and releases the current stream.
func (p *Player) Stop() {
	p.mu.Lock()
	p.stopStreamLocked()
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
		p.volume.Volume = gainToVolume(level) + p.rgOffset
		speaker.Unlock()
	}
}

// SetReplayGain turns per-track ReplayGain on/off and re-applies it to the
// currently-loaded track immediately (the tag read happens off the lock).
func (p *Player) SetReplayGain(on bool) {
	p.mu.Lock()
	p.replayGain = on
	var path string
	if p.index >= 0 && p.index < len(p.queue) {
		path = p.queue[p.index].AbsPath()
	}
	hasVol := p.volume != nil
	p.mu.Unlock()

	if !hasVol {
		return
	}
	off := 0.0
	if on && path != "" {
		off = replayGainOffset(path)
	}
	p.mu.Lock()
	p.rgOffset = off
	if p.volume != nil {
		speaker.Lock()
		p.volume.Volume = gainToVolume(p.gain) + p.rgOffset
		speaker.Unlock()
	}
	p.mu.Unlock()
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

// HasStream reports whether a stream is loaded (playing or paused) - i.e. there
// is something for TogglePause to act on. It is false after Stop, which tears
// down the stream but keeps the queue/index, so the Play button can tell
// "resume the paused track" from "start the stopped track fresh".
func (p *Player) HasStream() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ctrl != nil
}

// ResumeCurrent (re)starts playback of the current queue position from the top.
// It backs the Play button after Stop, which clears the stream but leaves the
// queue/index in place. Returns false if no queue is loaded (the caller then
// starts a fresh queue from the selection).
func (p *Player) ResumeCurrent() bool {
	p.mu.Lock()
	if p.index < 0 || p.index >= len(p.queue) {
		p.mu.Unlock()
		return false
	}
	p.playLocked()
	p.mu.Unlock()
	p.fireChange() // AFTER unlocking - see fireChange's contract
	return true
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
