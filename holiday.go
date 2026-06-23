// Package main - holiday.go is an undocumented seasonal easter egg: on a handful of
// US holidays the KrankyBear icon (app/dock, window title bar, system tray, and the
// in-window logo) is swapped for a holiday-themed bear, reverting the next day.
//
// The portraits live in assets/images/KrankyBearHoliday*.png (256x256, embedded here
// so they ship in the binary - the loose files aren't read from disk). Like easter.go,
// they're embedded separately from bundled.go so they don't advertise themselves. The
// date is evaluated once at launch; keep this out of the help/about text.
package main

import (
	"embed"
	"log"
	"os"
	"time"

	"fyne.io/fyne/v2"
)

//go:embed assets/images/KrankyBearHoliday*.png
var holidayFS embed.FS

// holidayIconResolved/Cache memoize the launch-time lookup so the four icon set-points
// all reuse the same resolved resource.
var (
	holidayIconResolved bool
	holidayIconCache    fyne.Resource
)

// holidayAppIcon returns today's holiday-themed bear, or the default app icon on
// ordinary days. Resolved once at launch (the gag is a launch-time treat; it doesn't
// re-evaluate if the app is left running across midnight).
func holidayAppIcon() fyne.Resource {
	if holidayIconResolved {
		return holidayIconCache
	}
	holidayIconResolved = true
	holidayIconCache = resourceKrankyBearMediaPlayerPng
	if name := holidayIconFile(holidayNow()); name != "" {
		if data, err := holidayFS.ReadFile("assets/images/" + name); err == nil {
			holidayIconCache = fyne.NewStaticResource(name, data)
		}
	}
	return holidayIconCache
}

// holidayNow returns the date used for the holiday lookup: normally time.Now(), but the
// KBMP_DATE env var overrides it so the seasonal icons can be tested without waiting for
// the calendar - e.g. KBMP_DATE=2026-12-25 ./mediaplayer (also accepts MM/DD/YYYY). An
// unparseable value is logged and ignored. Harmless in production (no-op unless set).
func holidayNow() time.Time {
	s := os.Getenv("KBMP_DATE")
	if s == "" {
		return time.Now()
	}
	for _, layout := range []string{"2006-01-02", "01/02/2006", "1/2/2006"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t
		}
	}
	log.Printf("KBMP_DATE=%q not understood (use YYYY-MM-DD or MM/DD/YYYY); using today", s)
	return time.Now()
}

// holidayIconFile maps a date to its KrankyBearHoliday*.png filename, or "" for an
// ordinary day. Fixed-date holidays first, then the movable feasts. Uses local time
// so it lands on the user's calendar day.
func holidayIconFile(t time.Time) string {
	y, m, d := t.Year(), t.Month(), t.Day()
	switch {
	case m == time.January && d == 1:
		return "KrankyBearHolidayNewYear.png"
	case m == time.March && d == 17:
		return "KrankyBearHolidayStPatrick.png"
	case m == time.July && d == 4:
		return "KrankyBearHolidayIndependence.png"
	case m == time.November && d == 11:
		return "KrankyBearHolidayVeteran.png"
	case m == time.December && d == 24:
		return "KrankyBearHolidayChristmasEveGrinch.png"
	case m == time.December && d == 25:
		return "KrankyBearHolidayChristmas.png"
	}
	switch {
	case sameDay(t, easterSunday(y)):
		return "KrankyBearHolidayEaster.png"
	case sameDay(t, lastWeekdayOfMonth(y, time.May, time.Monday)): // Memorial Day
		return "KrankyBearHolidayMemorial.png"
	case sameDay(t, nthWeekdayOfMonth(y, time.September, time.Monday, 1)): // Labor Day
		return "KrankyBearHolidayLabor.png"
	case sameDay(t, nthWeekdayOfMonth(y, time.November, time.Thursday, 4)): // Thanksgiving
		return "KrankyBearHolidayThanksgiving.png"
	}
	return ""
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// nthWeekdayOfMonth returns the date of the nth (1-based) given weekday in month.
func nthWeekdayOfMonth(year int, month time.Month, wd time.Weekday, n int) time.Time {
	first := time.Date(year, month, 1, 0, 0, 0, 0, time.Local)
	offset := (int(wd) - int(first.Weekday()) + 7) % 7
	return time.Date(year, month, 1+offset+(n-1)*7, 0, 0, 0, 0, time.Local)
}

// lastWeekdayOfMonth returns the date of the last given weekday in month.
func lastWeekdayOfMonth(year int, month time.Month, wd time.Weekday) time.Time {
	last := time.Date(year, month+1, 1, 0, 0, 0, 0, time.Local).AddDate(0, 0, -1)
	offset := (int(last.Weekday()) - int(wd) + 7) % 7
	return last.AddDate(0, 0, -offset)
}

// easterSunday computes Western (Gregorian) Easter for the year via the Anonymous
// Gregorian algorithm (Meeus/Jones/Butcher).
func easterSunday(year int) time.Time {
	a := year % 19
	b := year / 100
	c := year % 100
	d := b / 4
	e := b % 4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i := c / 4
	k := c % 4
	l := (32 + 2*e + 2*i - h - k) % 7
	mth := (a + 11*h + 22*l) / 451
	month := (h + l - 7*mth + 114) / 31
	day := ((h + l - 7*mth + 114) % 31) + 1
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.Local)
}
