package main

import (
	"strings"
	"testing"
)

// TestEasterEggQuip checks the quip combines a music line and a dad joke.
func TestEasterEggQuip(t *testing.T) {
	q := easterEggQuip()
	if strings.TrimSpace(q) == "" {
		t.Fatal("easterEggQuip returned empty")
	}
	if !strings.Contains(q, "— — —") {
		t.Errorf("expected the quip/dad-joke divider, got %q", q)
	}
}

// TestEggPortraitsEmbedded verifies the aging-gag portraits are bundled.
func TestEggPortraitsEmbedded(t *testing.T) {
	if got := len(eggPortraits()); got == 0 {
		t.Error("no easter-egg portraits embedded (assets/eggs/*.png)")
	}
}
