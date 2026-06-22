package main

import (
	"math"
	"reflect"
	"testing"
)

// runBalance streams the input through a balance stage at the given position,
// reusing sliceStreamer from eq_test.go (same package).
func runBalance(in [][2]float64, pos float64) [][2]float64 {
	b := newBalanceStreamer(&sliceStreamer{data: in}, pos)
	out := make([][2]float64, 0, len(in))
	buf := make([][2]float64, 512)
	for {
		n, ok := b.Stream(buf)
		out = append(out, buf[:n]...)
		if !ok {
			break
		}
	}
	return out
}

// TestBalanceGainsLaw checks the center/full-left/full-right gain mapping and clamping.
func TestBalanceGainsLaw(t *testing.T) {
	cases := []struct {
		pos, l, r float64
	}{
		{0, 1, 1},      // center: unity both
		{-1, 1, 0},     // full left: right silenced
		{1, 0, 1},      // full right: left silenced
		{-0.5, 1, 0.5}, // half left: right at half, left unity
		{0.5, 0.5, 1},  // half right
		{-2, 1, 0},     // clamps to full left
		{2, 0, 1},      // clamps to full right
	}
	for _, c := range cases {
		l, r := balanceGains(c.pos)
		if math.Abs(l-c.l) > 1e-9 || math.Abs(r-c.r) > 1e-9 {
			t.Errorf("balanceGains(%v) = (%v,%v), want (%v,%v)", c.pos, l, r, c.l, c.r)
		}
	}
}

// TestBalanceCentrePassthrough verifies center balance leaves samples untouched.
func TestBalanceCentrePassthrough(t *testing.T) {
	in := [][2]float64{{0.1, 0.2}, {-0.3, 0.4}, {0.5, -0.6}}
	out := runBalance(in, 0)
	if !reflect.DeepEqual(in, out) {
		t.Errorf("center balance altered samples: %v -> %v", in, out)
	}
}

// TestBalanceFullLeftSilencesRight checks full-left keeps the left channel and
// silences the right.
func TestBalanceFullLeftSilencesRight(t *testing.T) {
	in := [][2]float64{{0.1, 0.9}, {-0.3, 0.4}, {0.5, -0.6}}
	out := runBalance(in, -1)
	for i := range out {
		if out[i][0] != in[i][0] {
			t.Errorf("full left changed the left channel at %d: %v -> %v", i, in[i][0], out[i][0])
		}
		if out[i][1] != 0 {
			t.Errorf("full left did not silence the right channel at %d: got %v", i, out[i][1])
		}
	}
}
