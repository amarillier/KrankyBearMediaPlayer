package main

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

// TestIntegratedLUFSGating checks the gating math without decoding audio.
func TestIntegratedLUFSGating(t *testing.T) {
	// Uniform blocks: integrated loudness = block loudness = -0.691 + 10log10(z).
	lufs, ok := integratedLUFS([]float64{1, 1, 1, 1})
	if !ok || math.Abs(lufs-(-0.691)) > 1e-9 {
		t.Fatalf("uniform blocks: got (%v,%v), want -0.691", lufs, ok)
	}

	// Silence (zero energy) gates out completely → not measurable.
	if _, ok := integratedLUFS([]float64{0, 0, 0}); ok {
		t.Error("all-silence should be unmeasurable")
	}

	// Relative gate drops the quiet blocks (~ -30 LUFS) far below the loud ones,
	// so the integrated value tracks the loud blocks.
	mixed := []float64{1, 1, 1, 1, 1, 1, 1, 1, 1e-3, 1e-3}
	lufs, ok = integratedLUFS(mixed)
	if !ok || math.Abs(lufs-(-0.691)) > 0.2 {
		t.Errorf("relative gate: got (%v,%v), want ≈ -0.691", lufs, ok)
	}
}

// TestGainFromBlocksScaling verifies a 4× energy (≈ +6 dB louder) signal yields a
// gain ~6 dB lower, and silence yields 0.
func TestGainFromBlocksScaling(t *testing.T) {
	g1 := gainFromBlocks([]float64{1, 1, 1, 1})
	g4 := gainFromBlocks([]float64{4, 4, 4, 4})
	if d := g1 - g4; math.Abs(d-6.0206) > 0.01 {
		t.Errorf("4× energy should be ~6.02 dB quieter gain: diff=%v", d)
	}
	if g := gainFromBlocks(nil); g != 0 {
		t.Errorf("silence gain = %v, want 0", g)
	}
}

// TestReplayGainWriteRoundTripMP3 writes RG tags then reads them back, and checks an
// existing non-RG tag survives.
func TestReplayGainWriteRoundTripMP3(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.mp3")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeTrackTags(path, trackTags{Title: "Keep", Artist: "A"}); err != nil {
		t.Fatalf("seed tags: %v", err)
	}
	checkRGRoundTrip(t, path)
}

func TestReplayGainWriteRoundTripFLAC(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.flac")
	writeMinimalFLAC(t, path)
	if err := writeTrackTags(path, trackTags{Title: "Keep", Artist: "A"}); err != nil {
		t.Fatalf("seed tags: %v", err)
	}
	checkRGRoundTrip(t, path)
}

func checkRGRoundTrip(t *testing.T, path string) {
	t.Helper()
	if err := writeReplayGainTags(path, -6.50, 0.900000, -7.00, 0.950000); err != nil {
		t.Fatalf("writeReplayGainTags: %v", err)
	}

	// Track gain (preferAlbum=false).
	tg, tp, ok := readReplayGainMode(path, false)
	if !ok || math.Abs(tg-(-6.50)) > 0.01 || math.Abs(tp-0.9) > 1e-5 {
		t.Errorf("track RG: got (%v,%v,%v), want (-6.50,0.9,true)", tg, tp, ok)
	}
	// Album gain (preferAlbum=true).
	ag, ap, ok := readReplayGainMode(path, true)
	if !ok || math.Abs(ag-(-7.00)) > 0.01 || math.Abs(ap-0.95) > 1e-5 {
		t.Errorf("album RG: got (%v,%v,%v), want (-7.00,0.95,true)", ag, ap, ok)
	}
	// The pre-existing title must survive the RG write.
	tt, err := readTrackTags(path)
	if err != nil {
		t.Fatalf("readTrackTags: %v", err)
	}
	if tt.Title != "Keep" {
		t.Errorf("RG write clobbered other tags: Title=%q", tt.Title)
	}
}
