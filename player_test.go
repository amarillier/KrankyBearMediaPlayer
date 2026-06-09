package main

import "testing"

// TestRebuildOrderSequential: with shuffle off, the play order is the identity
// permutation and pos points at the current track.
func TestRebuildOrderSequential(t *testing.T) {
	p := &Player{queue: make([]Track, 5)}
	p.rebuildOrderLocked(3)
	for i, q := range p.order {
		if q != i {
			t.Fatalf("order[%d] = %d, want identity", i, q)
		}
	}
	if p.pos != 3 {
		t.Fatalf("pos = %d, want 3 (the current track)", p.pos)
	}
}

// TestRebuildOrderShuffle: with shuffle on, the order is a permutation of all
// queue indices, the current track is moved to the front, and pos is 0.
func TestRebuildOrderShuffle(t *testing.T) {
	p := &Player{queue: make([]Track, 6), shuffle: true}
	p.rebuildOrderLocked(4)

	if len(p.order) != 6 {
		t.Fatalf("order length = %d, want 6", len(p.order))
	}
	if p.order[0] != 4 {
		t.Fatalf("order[0] = %d, want current track 4 at front", p.order[0])
	}
	if p.pos != 0 {
		t.Fatalf("pos = %d, want 0 after shuffle", p.pos)
	}
	seen := make(map[int]bool, 6)
	for _, q := range p.order {
		if q < 0 || q >= 6 {
			t.Fatalf("order contains out-of-range index %d", q)
		}
		if seen[q] {
			t.Fatalf("order contains duplicate index %d", q)
		}
		seen[q] = true
	}
	if len(seen) != 6 {
		t.Fatalf("order is not a full permutation: %v", p.order)
	}
}

// playerWithQueue builds a Player whose queue holds tracks with the given IDs,
// in sequential (un-shuffled) play order, with the current track at pos/index.
func playerWithQueue(ids []int64, current int) *Player {
	p := &Player{index: current, pos: current}
	p.queue = make([]Track, len(ids))
	p.order = make([]int, len(ids))
	for i, id := range ids {
		p.queue[i] = Track{ID: id}
		p.order[i] = i
	}
	return p
}

// queueIDs returns the track IDs in play order, via the same path the UI uses.
func queueIDs(p *Player) []int64 {
	tracks, _ := p.QueueView()
	ids := make([]int64, len(tracks))
	for i, t := range tracks {
		ids[i] = t.ID
	}
	return ids
}

func equalIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestRemoveAt covers removing a track before, after, and equal to the current
// one, checking the queue contents and the reported current position stay right.
func TestRemoveAt(t *testing.T) {
	// Remove an upcoming track (after current): current is unaffected.
	p := playerWithQueue([]int64{10, 11, 12, 13}, 1)
	p.RemoveAt(3)
	if got := queueIDs(p); !equalIDs(got, []int64{10, 11, 12}) {
		t.Fatalf("after RemoveAt(3): queue %v, want [10 11 12]", got)
	}
	if _, cur := p.QueueView(); cur != 1 {
		t.Fatalf("after RemoveAt(3): current = %d, want 1 (track 11)", cur)
	}

	// Remove a track before the current one: current shifts down with it.
	p = playerWithQueue([]int64{10, 11, 12, 13}, 1)
	p.RemoveAt(0)
	if got := queueIDs(p); !equalIDs(got, []int64{11, 12, 13}) {
		t.Fatalf("after RemoveAt(0): queue %v, want [11 12 13]", got)
	}
	if _, cur := p.QueueView(); cur != 0 {
		t.Fatalf("after RemoveAt(0): current = %d, want 0 (track 11 still current)", cur)
	}

	// Remove the current track: it stops, so there is no current position.
	p = playerWithQueue([]int64{10, 11, 12, 13}, 1)
	p.RemoveAt(1)
	if got := queueIDs(p); !equalIDs(got, []int64{10, 12, 13}) {
		t.Fatalf("after RemoveAt(current): queue %v, want [10 12 13]", got)
	}
	if _, cur := p.QueueView(); cur != -1 {
		t.Fatalf("after RemoveAt(current): current = %d, want -1 (stopped)", cur)
	}
}

// TestMoveAt verifies reordering keeps the queue contents intact and keeps the
// current-track marker pointing at the same track in its new slot.
func TestMoveAt(t *testing.T) {
	p := playerWithQueue([]int64{10, 11, 12, 13}, 1) // current = track 11
	p.MoveAt(3, 0)                                   // move last to front
	if got := queueIDs(p); !equalIDs(got, []int64{13, 10, 11, 12}) {
		t.Fatalf("after MoveAt(3,0): queue %v, want [13 10 11 12]", got)
	}
	if _, cur := p.QueueView(); cur != 2 {
		t.Fatalf("after MoveAt(3,0): current = %d, want 2 (track 11 moved down)", cur)
	}
}
