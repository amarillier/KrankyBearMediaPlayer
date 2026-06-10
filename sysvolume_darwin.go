//go:build darwin

// Package main - sysvolume_darwin.go controls the macOS output device volume by
// calling the CoreAudio framework directly (via purego, no cgo), instead of
// shelling out to osascript the way itchyny/volume-go does on macOS.
//
// Why not osascript: "osascript -e 'set volume output muted true'" matches a
// known malware technique (muting system audio to mask activity), so endpoint
// security tools flag it; a recurring poll that spawns osascript every couple of
// seconds also looks anomalous. Driving CoreAudio in-process means no subprocess,
// no AppleScript, and nothing for those tools to trip on - for us and for anyone
// who runs the app inside a managed organisation.
//
// This is distinct from the per-stream playback gain in player.go: the gain only
// attenuates this app's audio, while these calls move the whole OS output level.
package main

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// CoreAudio constants. The property selectors/scopes are FourCharCodes.
const (
	kAudioObjectSystemObject          = 1
	kAudioObjectPropertyElementMain   = 0 // a.k.a. ...ElementMaster on older SDKs
	kAudioHardwareNoError             = 0
	kAudioObjectUnknown        uint32 = 0
)

var (
	kAudioHardwarePropertyDefaultOutputDevice = fourCC("dOut")
	kAudioObjectPropertyScopeGlobal           = fourCC("glob")
	kAudioObjectPropertyScopeOutput           = fourCC("outp")
	kAudioDevicePropertyVolumeScalar          = fourCC("volm")
	kAudioDevicePropertyMute                  = fourCC("mute")
)

// fourCC packs a 4-character code into a big-endian uint32, as CoreAudio expects.
func fourCC(s string) uint32 {
	return uint32(s[0])<<24 | uint32(s[1])<<16 | uint32(s[2])<<8 | uint32(s[3])
}

// audioObjectPropertyAddress mirrors the CoreAudio struct of the same name.
type audioObjectPropertyAddress struct {
	selector uint32
	scope    uint32
	element  uint32
}

var (
	audioObjectGetPropertyData func(objID uint32, addr unsafe.Pointer, qualSize uint32, qual unsafe.Pointer, dataSize unsafe.Pointer, data unsafe.Pointer) int32
	audioObjectSetPropertyData func(objID uint32, addr unsafe.Pointer, qualSize uint32, qual unsafe.Pointer, dataSize uint32, data unsafe.Pointer) int32
	audioObjectHasProperty     func(objID uint32, addr unsafe.Pointer) bool

	coreAudioOnce sync.Once
	coreAudioErr  error
)

// loadCoreAudio dlopens the CoreAudio framework and binds the property calls once.
func loadCoreAudio() error {
	coreAudioOnce.Do(func() {
		lib, err := purego.Dlopen(
			"/System/Library/Frameworks/CoreAudio.framework/CoreAudio",
			purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			coreAudioErr = fmt.Errorf("CoreAudio: load framework: %w", err)
			return
		}
		purego.RegisterLibFunc(&audioObjectGetPropertyData, lib, "AudioObjectGetPropertyData")
		purego.RegisterLibFunc(&audioObjectSetPropertyData, lib, "AudioObjectSetPropertyData")
		purego.RegisterLibFunc(&audioObjectHasProperty, lib, "AudioObjectHasProperty")
	})
	return coreAudioErr
}

// defaultOutputDevice returns the system's current default output device ID.
func defaultOutputDevice() (uint32, error) {
	addr := audioObjectPropertyAddress{
		selector: kAudioHardwarePropertyDefaultOutputDevice,
		scope:    kAudioObjectPropertyScopeGlobal,
		element:  kAudioObjectPropertyElementMain,
	}
	dev := kAudioObjectUnknown
	size := uint32(4)
	st := audioObjectGetPropertyData(kAudioObjectSystemObject, unsafe.Pointer(&addr), 0, nil, unsafe.Pointer(&size), unsafe.Pointer(&dev))
	if st != kAudioHardwareNoError {
		return 0, fmt.Errorf("CoreAudio: default output device: status %d", st)
	}
	if dev == kAudioObjectUnknown {
		return 0, errors.New("CoreAudio: no default output device")
	}
	return dev, nil
}

func hasProp(dev uint32, a audioObjectPropertyAddress) bool {
	return audioObjectHasProperty(dev, unsafe.Pointer(&a))
}

func getFloat32(dev uint32, a audioObjectPropertyAddress) (float32, error) {
	var v float32
	size := uint32(4)
	st := audioObjectGetPropertyData(dev, unsafe.Pointer(&a), 0, nil, unsafe.Pointer(&size), unsafe.Pointer(&v))
	if st != kAudioHardwareNoError {
		return 0, fmt.Errorf("CoreAudio: get float: status %d", st)
	}
	return v, nil
}

func setFloat32(dev uint32, a audioObjectPropertyAddress, v float32) error {
	st := audioObjectSetPropertyData(dev, unsafe.Pointer(&a), 0, nil, 4, unsafe.Pointer(&v))
	if st != kAudioHardwareNoError {
		return fmt.Errorf("CoreAudio: set float: status %d", st)
	}
	return nil
}

func getUint32(dev uint32, a audioObjectPropertyAddress) (uint32, error) {
	var v uint32
	size := uint32(4)
	st := audioObjectGetPropertyData(dev, unsafe.Pointer(&a), 0, nil, unsafe.Pointer(&size), unsafe.Pointer(&v))
	if st != kAudioHardwareNoError {
		return 0, fmt.Errorf("CoreAudio: get uint32: status %d", st)
	}
	return v, nil
}

func setUint32(dev uint32, a audioObjectPropertyAddress, v uint32) error {
	st := audioObjectSetPropertyData(dev, unsafe.Pointer(&a), 0, nil, 4, unsafe.Pointer(&v))
	if st != kAudioHardwareNoError {
		return fmt.Errorf("CoreAudio: set uint32: status %d", st)
	}
	return nil
}

// volumeAddr returns the volume-scalar property address for an element.
func volumeAddr(element uint32) audioObjectPropertyAddress {
	return audioObjectPropertyAddress{
		selector: kAudioDevicePropertyVolumeScalar,
		scope:    kAudioObjectPropertyScopeOutput,
		element:  element,
	}
}

// muteAddr returns the mute property address (main element).
func muteAddr() audioObjectPropertyAddress {
	return audioObjectPropertyAddress{
		selector: kAudioDevicePropertyMute,
		scope:    kAudioObjectPropertyScopeOutput,
		element:  kAudioObjectPropertyElementMain,
	}
}

// sysVolumeGet returns the current OS output volume as a percentage (0..100).
// It reads the main volume element, falling back to channel 1 for devices that
// only expose per-channel volume.
func sysVolumeGet() (int, error) {
	if err := loadCoreAudio(); err != nil {
		return 0, err
	}
	dev, err := defaultOutputDevice()
	if err != nil {
		return 0, err
	}
	for _, el := range []uint32{kAudioObjectPropertyElementMain, 1} {
		a := volumeAddr(el)
		if hasProp(dev, a) {
			v, err := getFloat32(dev, a)
			if err != nil {
				return 0, err
			}
			return int(v*100 + 0.5), nil
		}
	}
	return 0, errors.New("CoreAudio: device has no readable volume control")
}

// sysVolumeSet sets the OS output volume from a percentage (clamped to 0..100).
// It writes the main element when present, otherwise both stereo channels.
func sysVolumeSet(pct int) error {
	if pct < 0 {
		pct = 0
	} else if pct > 100 {
		pct = 100
	}
	if err := loadCoreAudio(); err != nil {
		return err
	}
	dev, err := defaultOutputDevice()
	if err != nil {
		return err
	}
	v := float32(pct) / 100

	if main := volumeAddr(kAudioObjectPropertyElementMain); hasProp(dev, main) {
		return setFloat32(dev, main, v)
	}
	var set bool
	var firstErr error
	for _, ch := range []uint32{1, 2} {
		a := volumeAddr(ch)
		if !hasProp(dev, a) {
			continue
		}
		if err := setFloat32(dev, a, v); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		set = true
	}
	if firstErr != nil {
		return firstErr
	}
	if !set {
		return errors.New("CoreAudio: device has no settable volume control")
	}
	return nil
}

// sysMutedGet reports whether the OS output is currently muted.
func sysMutedGet() (bool, error) {
	if err := loadCoreAudio(); err != nil {
		return false, err
	}
	dev, err := defaultOutputDevice()
	if err != nil {
		return false, err
	}
	a := muteAddr()
	if !hasProp(dev, a) {
		return false, errors.New("CoreAudio: device has no mute control")
	}
	v, err := getUint32(dev, a)
	if err != nil {
		return false, err
	}
	return v != 0, nil
}

// sysSetMuted mutes or unmutes the OS output.
func sysSetMuted(muted bool) error {
	if err := loadCoreAudio(); err != nil {
		return err
	}
	dev, err := defaultOutputDevice()
	if err != nil {
		return err
	}
	a := muteAddr()
	if !hasProp(dev, a) {
		return errors.New("CoreAudio: device has no mute control")
	}
	var v uint32
	if muted {
		v = 1
	}
	return setUint32(dev, a, v)
}
