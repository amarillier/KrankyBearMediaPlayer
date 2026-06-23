package main

import (
	"testing"
	"time"
)

// TestHolidayIconFile checks the date→filename mapping, including the movable feasts,
// and that an ordinary day yields no icon.
func TestHolidayIconFile(t *testing.T) {
	d := func(y int, m time.Month, day int) time.Time {
		return time.Date(y, m, day, 12, 0, 0, 0, time.Local)
	}
	cases := []struct {
		when time.Time
		want string
	}{
		{d(2026, time.January, 1), "KrankyBearHolidayNewYear.png"},
		{d(2026, time.March, 17), "KrankyBearHolidayStPatrick.png"},
		{d(2026, time.July, 4), "KrankyBearHolidayIndependence.png"},
		{d(2026, time.November, 11), "KrankyBearHolidayVeteran.png"},
		{d(2026, time.December, 24), "KrankyBearHolidayChristmasEveGrinch.png"},
		{d(2026, time.December, 25), "KrankyBearHolidayChristmas.png"},
		{d(2026, time.April, 5), "KrankyBearHolidayEaster.png"},           // Easter 2026
		{d(2027, time.March, 28), "KrankyBearHolidayEaster.png"},          // Easter 2027
		{d(2026, time.May, 25), "KrankyBearHolidayMemorial.png"},          // last Mon of May 2026
		{d(2026, time.September, 7), "KrankyBearHolidayLabor.png"},        // first Mon of Sep 2026
		{d(2026, time.November, 26), "KrankyBearHolidayThanksgiving.png"}, // 4th Thu of Nov 2026
		{d(2026, time.June, 23), ""},                                      // ordinary day
		{d(2026, time.December, 26), ""},                                  // day after Christmas
	}
	for _, c := range cases {
		if got := holidayIconFile(c.when); got != c.want {
			t.Errorf("holidayIconFile(%s) = %q, want %q", c.when.Format("2006-01-02"), got, c.want)
		}
	}
}

// TestHolidayIconsEmbedded confirms every filename the mapping can return is actually
// present in the embedded FS (guards against a rename/typo silently falling back to
// the default icon).
func TestHolidayIconsEmbedded(t *testing.T) {
	names := []string{
		"KrankyBearHolidayNewYear.png", "KrankyBearHolidayStPatrick.png",
		"KrankyBearHolidayIndependence.png", "KrankyBearHolidayVeteran.png",
		"KrankyBearHolidayChristmasEveGrinch.png", "KrankyBearHolidayChristmas.png",
		"KrankyBearHolidayEaster.png", "KrankyBearHolidayMemorial.png",
		"KrankyBearHolidayLabor.png", "KrankyBearHolidayThanksgiving.png",
	}
	for _, n := range names {
		if _, err := holidayFS.ReadFile("assets/images/" + n); err != nil {
			t.Errorf("embedded holiday icon missing: %s (%v)", n, err)
		}
	}
}
