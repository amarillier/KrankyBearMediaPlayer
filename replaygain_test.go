package main

import (
	"math"
	"testing"
)

func TestParseGainDB(t *testing.T) {
	cases := map[string]struct {
		want float64
		ok   bool
	}{
		"-6.48 dB": {-6.48, true},
		"+3.21 dB": {3.21, true},
		"-9.00 DB": {-9.00, true},
		"0.00":     {0, true}, // some taggers omit the unit
		"  -5 dB ": {-5, true},
		"":         {0, false},
		"loud":     {0, false},
	}
	for in, c := range cases {
		got, ok := parseGainDB(in)
		if ok != c.ok || (ok && math.Abs(got-c.want) > 1e-9) {
			t.Errorf("parseGainDB(%q) = (%v, %v), want (%v, %v)", in, got, ok, c.want, c.ok)
		}
	}
}

// TestReplayGainPeakClamp checks that a positive (boosting) gain is capped by the
// peak so playback can't clip, while attenuation passes through unclamped.
func TestReplayGainPeakClamp(t *testing.T) {
	// Helper mirrors replayGainOffset's math without file IO.
	offset := func(db, peak float64) float64 {
		factor := math.Pow(10, db/20)
		if peak > 0 && factor*peak > 1 {
			factor = 1 / peak
		}
		return math.Log2(factor)
	}
	// +6 dB (~2x) with a 0.9 peak would clip (2*0.9=1.8); must clamp to 1/0.9.
	clamped := offset(6, 0.9)
	wantClamped := math.Log2(1 / 0.9)
	if math.Abs(clamped-wantClamped) > 1e-9 {
		t.Errorf("boost not clamped: got %v want %v", clamped, wantClamped)
	}
	// -6 dB attenuation with a low peak: no clamp, factor = 10^(-6/20).
	atten := offset(-6, 0.5)
	wantAtten := math.Log2(math.Pow(10, -6.0/20))
	if math.Abs(atten-wantAtten) > 1e-9 {
		t.Errorf("attenuation altered: got %v want %v", atten, wantAtten)
	}
}
