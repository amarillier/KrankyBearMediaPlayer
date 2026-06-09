package main

import "testing"

// TestPlayCountLabelMapping round-trips the threshold dropdown labels/percentages
// and confirms unknown inputs fall back to the default.
func TestPlayCountLabelMapping(t *testing.T) {
	for _, o := range playCountOptions {
		if got := playCountLabelFor(o.Pct); got != o.Label {
			t.Errorf("playCountLabelFor(%d) = %q, want %q", o.Pct, got, o.Label)
		}
		if got := playCountPctFor(o.Label); got != o.Pct {
			t.Errorf("playCountPctFor(%q) = %d, want %d", o.Label, got, o.Pct)
		}
	}
	if got := playCountPctFor("bogus"); got != defaultPlayCountPct {
		t.Errorf("playCountPctFor(bogus) = %d, want default %d", got, defaultPlayCountPct)
	}
	if got := playCountLabelFor(999); got != "50%" {
		t.Errorf("playCountLabelFor(unknown) = %q, want fallback 50%%", got)
	}
}
