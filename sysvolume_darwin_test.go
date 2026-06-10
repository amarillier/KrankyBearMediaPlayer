//go:build darwin

package main

import "testing"

// TestCoreAudioRead exercises the CoreAudio plumbing read-only (it never changes
// the system volume). It skips if there's no usable output device (e.g. headless
// CI) so it can't fail spuriously there.
func TestCoreAudioRead(t *testing.T) {
	vol, err := sysVolumeGet()
	if err != nil {
		t.Skipf("no readable output device: %v", err)
	}
	if vol < 0 || vol > 100 {
		t.Errorf("volume out of range: %d", vol)
	}
	if _, err := sysMutedGet(); err != nil {
		t.Logf("mute state unavailable (some devices lack it): %v", err)
	}
}
