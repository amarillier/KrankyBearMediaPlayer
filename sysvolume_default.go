//go:build !darwin

// Package main - sysvolume_default.go controls the OS output device volume on
// every platform except macOS, backed by itchyny/volume-go. On Windows that's
// in-process CoreAudio COM (go-wca); on Linux it's amixer. macOS deliberately
// uses a separate CoreAudio implementation (sysvolume_darwin.go) instead of
// volume-go's osascript shell-outs - see that file for why.
//
// This is distinct from the per-stream playback gain in player.go: the gain only
// attenuates this app's audio, while these calls move the whole OS output level.
package main

import volume "github.com/itchyny/volume-go"

// sysVolumeGet returns the current OS output volume as a percentage (0..100).
func sysVolumeGet() (int, error) {
	return volume.GetVolume()
}

// sysVolumeSet sets the OS output volume from a percentage (clamped to 0..100).
func sysVolumeSet(pct int) error {
	if pct < 0 {
		pct = 0
	} else if pct > 100 {
		pct = 100
	}
	return volume.SetVolume(pct)
}

// sysMutedGet reports whether the OS output is currently muted.
func sysMutedGet() (bool, error) {
	return volume.GetMuted()
}

// sysSetMuted mutes or unmutes the OS output.
func sysSetMuted(muted bool) error {
	if muted {
		return volume.Mute()
	}
	return volume.Unmute()
}
