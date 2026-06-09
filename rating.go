// Package main - rating.go centralizes star-rating logic.
//
// Two ways to rate, as the user requested:
//   - Manual: an explicit 1..5 star rating stored on the track.
//   - Auto:   derived from play count when no manual rating is set, so that
//     playing something 5 times makes it a 5-star track on its own.
//
// EffectiveRating (Track.EffectiveRating / effRatingExpr in SQL) is what the
// UI shows and what filters operate on: manual if present, else auto.
package main

// autoRating maps a play count to a star rating, capped at 5. Kept trivial and
// linear for now (1 play = 1 star ... 5+ plays = 5 stars); this is the Go twin
// of effRatingExpr's MIN(play_count, 5) in db.go. If thresholds ever become
// configurable, change both together.
func autoRating(playCount int) int {
	if playCount > 5 {
		return 5
	}
	if playCount < 0 {
		return 0
	}
	return playCount
}

// Filter selects which tracks the library view shows, by effective rating.
type Filter int

const (
	FilterAll      Filter = iota // everything
	FilterUnplayed               // never played and never manually rated
	FilterAtLeast1               // effective rating >= 1 (i.e. played or rated)
	FilterAtLeast2
	FilterAtLeast3
	FilterAtLeast4
	FilterExactly1
	FilterExactly2
	FilterExactly3
	FilterExactly4
	FilterExactly5
)

// where returns the SQL WHERE clause (without the "WHERE" keyword) for the
// filter, or "" for no restriction. effRatingExpr lives in db.go.
func (f Filter) where() string {
	switch f {
	case FilterUnplayed:
		return "t.play_count = 0 AND t.rating IS NULL"
	case FilterAtLeast1:
		return effRatingExpr + " >= 1"
	case FilterAtLeast2:
		return effRatingExpr + " >= 2"
	case FilterAtLeast3:
		return effRatingExpr + " >= 3"
	case FilterAtLeast4:
		return effRatingExpr + " >= 4"
	case FilterExactly1:
		return effRatingExpr + " = 1"
	case FilterExactly2:
		return effRatingExpr + " = 2"
	case FilterExactly3:
		return effRatingExpr + " = 3"
	case FilterExactly4:
		return effRatingExpr + " = 4"
	case FilterExactly5:
		return effRatingExpr + " = 5"
	default:
		return ""
	}
}

// filterOptions is the ordered list of (label, Filter) pairs for the UI's
// filter dropdown. effRatingExpr uses MIN(play_count,5), so these read against
// the effective rating.
var filterOptions = []struct {
	Label  string
	Filter Filter
}{
	{"All tracks", FilterAll},
	{"Unplayed (no stars)", FilterUnplayed},
	{"5 stars", FilterExactly5},
	{"4 stars", FilterExactly4},
	{"3 stars", FilterExactly3},
	{"2 stars", FilterExactly2},
	{"1 star", FilterExactly1},
	{"4 stars & up", FilterAtLeast4},
	{"3 stars & up", FilterAtLeast3},
	{"2 stars & up", FilterAtLeast2},
	{"Played (1+ stars)", FilterAtLeast1},
}

// filterByLabel returns the Filter for a dropdown label.
func filterByLabel(label string) Filter {
	for _, o := range filterOptions {
		if o.Label == label {
			return o.Filter
		}
	}
	return FilterAll
}

// filterLabelFor returns the dropdown label for a filter (for summaries).
func filterLabelFor(f Filter) string {
	for _, o := range filterOptions {
		if o.Filter == f {
			return o.Label
		}
	}
	return ""
}

// filterLabels returns just the labels, for building the dropdown.
func filterLabels() []string {
	labels := make([]string, len(filterOptions))
	for i, o := range filterOptions {
		labels[i] = o.Label
	}
	return labels
}

// starString renders an effective rating as filled/empty stars for display.
// Zero (unplayed/unrated) shows as five empty stars.
func starString(rating int) string {
	const filled, empty = "★", "☆"
	out := ""
	for i := 1; i <= 5; i++ {
		if i <= rating {
			out += filled
		} else {
			out += empty
		}
	}
	return out
}
