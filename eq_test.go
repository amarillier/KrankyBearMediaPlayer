package main

import (
	"math"
	"reflect"
	"testing"
)

// sliceStreamer feeds prepared samples for EQ tests.
type sliceStreamer struct {
	data [][2]float64
	pos  int
}

func (s *sliceStreamer) Stream(buf [][2]float64) (int, bool) {
	if s.pos >= len(s.data) {
		return 0, false
	}
	n := copy(buf, s.data[s.pos:])
	s.pos += n
	return n, true
}

func (s *sliceStreamer) Err() error { return nil }

func runEQ(in [][2]float64, gains [eqBandCount]float64, enabled bool) [][2]float64 {
	eq := newEQStreamer(&sliceStreamer{data: in}, gains, enabled)
	out := make([][2]float64, 0, len(in))
	buf := make([][2]float64, 512)
	for {
		n, ok := eq.Stream(buf)
		out = append(out, buf[:n]...)
		if !ok {
			break
		}
	}
	return out
}

func rms(xs [][2]float64, from int) float64 {
	var sum float64
	var n int
	for i := from; i < len(xs); i++ {
		sum += xs[i][0] * xs[i][0]
		n++
	}
	if n == 0 {
		return 0
	}
	return math.Sqrt(sum / float64(n))
}

// TestPeakingZeroDBTransparent verifies a 0 dB peaking section is an identity filter.
func TestPeakingZeroDBTransparent(t *testing.T) {
	var b biquad
	b.setPeaking(1000, 0, eqQ, float64(playerSampleRate))
	in := []float64{0.1, -0.3, 0.5, 0.9, -0.7, 0.2, -0.1, 0.4}
	for _, x := range in {
		if y := b.process(x); math.Abs(y-x) > 1e-9 {
			t.Errorf("0 dB not transparent: in %v out %v", x, y)
		}
	}
}

// TestEQDisabledPassthrough verifies a disabled EQ returns samples unchanged.
func TestEQDisabledPassthrough(t *testing.T) {
	in := [][2]float64{{0.1, 0.2}, {-0.3, 0.4}, {0.5, -0.6}}
	gains := [eqBandCount]float64{6, 6, 6, 6, 6, 6, 6, 6, 6, 6}
	out := runEQ(in, gains, false)
	if !reflect.DeepEqual(in, out) {
		t.Errorf("disabled EQ altered samples: %v -> %v", in, out)
	}
}

// TestEQBoostsTargetBand checks that boosting the 1 kHz band raises a 1 kHz sine's
// level, while a flat EQ leaves it unchanged.
func TestEQBoostsTargetBand(t *testing.T) {
	const fs = float64(playerSampleRate)
	const freq = 1000.0 // == eqFreqs[5]
	n := int(fs)        // 1 second
	in := make([][2]float64, n)
	for i := 0; i < n; i++ {
		v := 0.3 * math.Sin(2*math.Pi*freq*float64(i)/fs)
		in[i] = [2]float64{v, v}
	}

	flat := runEQ(in, [eqBandCount]float64{}, true)
	var boostGains [eqBandCount]float64
	boostGains[5] = 12
	boosted := runEQ(in, boostGains, true)

	// Skip the filter's settling transient.
	skip := 2000
	flatRMS := rms(flat, skip)
	boostRMS := rms(boosted, skip)
	inRMS := rms(in, skip)

	if math.Abs(flatRMS-inRMS) > 1e-3 {
		t.Errorf("flat EQ changed the signal: in %.4f flat %.4f", inRMS, flatRMS)
	}
	if boostRMS <= flatRMS*1.2 {
		t.Errorf("boosting 1 kHz band didn't raise a 1 kHz sine: flat %.4f boosted %.4f", flatRMS, boostRMS)
	}
}

func TestGainsCSVRoundTrip(t *testing.T) {
	g := [eqBandCount]float64{-12, -3, 0, 1.5, 4, 6, -6, 2, 0, 12}
	got := gainsFromCSV(gainsToCSV(g))
	if got != g {
		t.Errorf("CSV round trip: %v -> %v", g, got)
	}
	// Out-of-range values are clamped.
	if c := gainsFromCSV("99,-99,0,0,0,0,0,0,0,0"); c[0] != eqMaxDB || c[1] != eqMinDB {
		t.Errorf("clamp failed: %v", c)
	}
}

func TestCustomPresetsJSON(t *testing.T) {
	m := map[string][eqBandCount]float64{
		"Mine":  {1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		"Quiet": {-2, -2, -2, -2, -2, -2, -2, -2, -2, -2},
	}
	got := customPresets(encodeCustomPresets(m))
	if !reflect.DeepEqual(got, m) {
		t.Errorf("custom preset round trip: %v -> %v", m, got)
	}
	if len(customPresets("")) != 0 || len(customPresets("not json")) != 0 {
		t.Error("empty/garbage JSON should yield no presets")
	}
}
