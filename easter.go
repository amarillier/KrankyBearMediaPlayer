// Package main - easter.go is an undocumented bit of fun: a small whimsical window
// triggered by clicking the bottom-left bear logo, or by re-opening About/Help while
// it's already on screen. Modelled on ../TaniumQuest/internal/ui/easter.go and
// KrankyBearTimer: a random music quip paired with one of Allan's portable dad jokes,
// and a portrait that "ages" the more you poke it in a session. Deliberately kept out
// of Help and the release notes.
package main

import (
	"embed"
	"fmt"
	"math/rand"
	"sort"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// eggFS holds the aging-gag portraits, embedded separately from bundled.go so they
// don't advertise themselves. Filenames sort 01→15 = young→old.
//
//go:embed assets/eggs/*.png
var eggFS embed.FS

// easterEggCount is the per-session trigger count; it drives which portrait shows so
// repeats escalate the gag. Package-level so the free functions showAbout/showHelp can
// trigger an egg too.
var easterEggCount int

// eggPortraits lazily loads and caches the embedded portraits, sorted by name.
var eggPortraitsCache []fyne.Resource

func eggPortraits() []fyne.Resource {
	if eggPortraitsCache != nil {
		return eggPortraitsCache
	}
	entries, err := eggFS.ReadDir("assets/eggs")
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, n := range names {
		if data, err := eggFS.ReadFile("assets/eggs/" + n); err == nil {
			eggPortraitsCache = append(eggPortraitsCache, fyne.NewStaticResource(n, data))
		}
	}
	return eggPortraitsCache
}

// dadJokes is Allan's portable joke pool — the same set as KrankyBearTimer/TaniumQuest
// so the brand follows him across apps.
var dadJokes = []string{
	"We're having Himalayan rabbit stew for dinner.\nI found Him a-layin in the middle of the road",
	"I went to the local zoo, but all they had was one dog.\nIt was a Shi-Tzu",
	"Where do rainbows go when they've been bad?\nTo prism, so they have time to reflect on what they've done",
	"Dogs can't operate MRI machines.\nBut catscan",
	"What do you call a dog who meditates?\nAware wolf",
	"Why did the old man fall down the well?\nHe couldn't see that well",
	"The other day I bought a thesaurus, but when I got home all the pages were blank.\nI have no words to describe how angry I am",
	"Why can't humans hear a dog whistle?\nBecause a dog can't whistle",
	"What is a dog's favorite form of transport?\nA waggin",
	"What's a forklift?\nUsually, food",
	"Did you know you can wear a canoe as a hat?\nIf you turn it over, it is capsized",
	"I was going to tell a time-traveling joke, but you didn't like it",
	"Why did the scarecrow win an award?\nBecause he was outstanding in his field",
	"Why don't skeletons fight each other?\nThey don't have the guts",
	"Why don't scientists trust atoms?\nBecause they make up everything",
	"What do you call fake spaghetti?\nAn impasta",
	"Davey Crockett was the only man ever to have three ears.\nA left ear, a right ear, and a wild front ear",
}

// musicQuips are short music-themed dad-joke-energy lines, the player's answer to
// TaniumQuest's wizard quips.
var musicQuips = []string{
	"Why did the music teacher need a ladder? To reach the high notes.",
	"I used to be a musician, but then I lost my keys.",
	"What's a skeleton's least favourite instrument? The trom-bone.",
	"Why did the song go to jail? It got caught in a treble.",
	"I'd tell you a joke about a broken MP3, but you wouldn't get the hook.",
	"What genre are walls? Heavy mortar.",
	"My playlist is so portable it has relative paths to my heart.",
	"Why are pirates great at karaoke? They hit the high C's.",
	"I tried to organise my library by mood, but it kept changing tempo.",
	"What do you call 5 stars on every track? Suspiciously generous.",
	"ReplayGain walked into a bar. Everyone was the same loudness. It was lovely.",
	"Why did the FLAC break up with the MP3? It wanted something lossless.",
	"I asked the bear to DJ. Now everything's bearable.",
	"Crossfade: because silence is the one track nobody rated.",
}

// easterEggQuip pairs a random music quip with a random dad joke.
func easterEggQuip() string {
	q := musicQuips[rand.Intn(len(musicQuips))]
	d := dadJokes[rand.Intn(len(dadJokes))]
	return q + "\n\n— — —\n\n" + d
}

// showEasterEgg opens a small celebratory window. The portrait walks the sorted set as
// the per-session count climbs (the gag "ages"), going random once past the last one.
func showEasterEgg(a fyne.App, banner string) {
	easterEggCount++

	ports := eggPortraits()
	var res fyne.Resource = resourceKrankyBearMediaPlayerPng
	switch {
	case len(ports) == 0:
		// fall back to the app icon
	case easterEggCount-1 < len(ports):
		res = ports[easterEggCount-1]
	default:
		res = ports[rand.Intn(len(ports))]
	}

	win := a.NewWindow(fmt.Sprintf("KrankyBear — Easter Egg #%d", easterEggCount))
	win.SetIcon(resourceKrankyBearMediaPlayerPng)

	img := canvas.NewImageFromResource(res)
	img.FillMode = canvas.ImageFillContain
	img.SetMinSize(fyne.NewSize(300, 300))

	title := widget.NewLabelWithStyle(banner, fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	quip := widget.NewLabel(easterEggQuip())
	quip.Alignment = fyne.TextAlignCenter
	quip.Wrapping = fyne.TextWrapWord

	closeBtn := widget.NewButton("Close", func() {
		forgetWindow(win)
		win.Close()
	})
	content := container.NewBorder(
		title,
		container.NewCenter(closeBtn),
		nil, nil,
		container.NewVBox(container.NewCenter(img), container.NewPadded(quip)),
	)

	win.SetContent(content)
	win.Resize(fyne.NewSize(420, 520))
	// Eggs hide/show with the main window like other secondary windows. Closing one
	// frees it and drops it from the registry (forgetWindow) so they don't accumulate.
	win.SetCloseIntercept(func() {
		forgetWindow(win)
		win.Close()
	})
	win.Show()
	win.CenterOnScreen()
	markWindowOpen(win)
}

// tappableImage is a canvas image that runs onTap when clicked and shows a pointer
// cursor on hover - used to make the bottom-left bear logo an easter-egg trigger.
// Mirrors tappableLabel (mainwindow.go).
type tappableImage struct {
	widget.BaseWidget
	img   *canvas.Image
	onTap func()
}

func newTappableImage(res fyne.Resource, minSize fyne.Size, onTap func()) *tappableImage {
	img := canvas.NewImageFromResource(res)
	img.FillMode = canvas.ImageFillContain
	img.SetMinSize(minSize)
	t := &tappableImage{img: img, onTap: onTap}
	t.ExtendBaseWidget(t)
	return t
}

func (t *tappableImage) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(t.img)
}

func (t *tappableImage) Tapped(_ *fyne.PointEvent) {
	if t.onTap != nil {
		t.onTap()
	}
}

func (t *tappableImage) Cursor() desktop.Cursor { return desktop.PointerCursor }
