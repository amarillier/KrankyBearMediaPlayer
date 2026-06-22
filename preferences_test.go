package main

import "testing"

// TestPlayCountLabelMapping round-trips each threshold percentage through its
// (translated) dropdown label and confirms unknown labels fall back to the default.
func TestPlayCountLabelMapping(t *testing.T) {
	for _, pct := range playCountThresholds {
		label := playCountLabelFor(pct)
		if got := playCountPctFor(label); got != pct {
			t.Errorf("playCountPctFor(%q) = %d, want %d", label, got, pct)
		}
	}
	if got := playCountPctFor("bogus"); got != defaultPlayCountPct {
		t.Errorf("playCountPctFor(bogus) = %d, want default %d", got, defaultPlayCountPct)
	}
}
