// Package main - rating.go centralizes star-rating logic.
//
// Ratings are manual-only: an explicit 1..5 star rating the user sets. Play count
// is tracked and shown in its own column (and is filterable), but it is NOT turned
// into stars - that auto-rating behaviour was dropped as confusing (clearing a
// rating now truly goes to no stars).
//
// EffectiveRating (Track.EffectiveRating / effRatingExpr in SQL) is what the UI
// shows and what the rating filters operate on: the manual rating, or 0.
package main

import "mediaplayer/internal/i18n"

// Filter selects which tracks the library view shows, by manual star rating.
type Filter int

const (
	FilterAll      Filter = iota // everything
	FilterUnrated                // no manual rating set
	FilterAtLeast1               // rating >= 1
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
	case FilterUnrated:
		return "t.rating IS NULL"
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

// filterOptions is the ordered list of (i18n key, Filter) pairs for the UI's
// filter dropdown. These read against the manual star rating. The Key is a stable
// catalog key (filter.rating.*); the visible label is translated at render time
// (filterOptions is a package var that inits before i18n.Init, so we cannot bake
// the translated string in here).
var filterOptions = []struct {
	Key    string
	Filter Filter
}{
	{"filter.rating.all", FilterAll},
	{"filter.rating.unrated", FilterUnrated},
	{"filter.rating.exactly5", FilterExactly5},
	{"filter.rating.exactly4", FilterExactly4},
	{"filter.rating.exactly3", FilterExactly3},
	{"filter.rating.exactly2", FilterExactly2},
	{"filter.rating.exactly1", FilterExactly1},
	{"filter.rating.atleast4", FilterAtLeast4},
	{"filter.rating.atleast3", FilterAtLeast3},
	{"filter.rating.atleast2", FilterAtLeast2},
	{"filter.rating.atleast1", FilterAtLeast1},
}

// filterByLabel returns the Filter for a (translated) dropdown label.
func filterByLabel(label string) Filter {
	for _, o := range filterOptions {
		if i18n.T(o.Key) == label {
			return o.Filter
		}
	}
	return FilterAll
}

// filterLabelFor returns the translated dropdown label for a filter (for summaries).
func filterLabelFor(f Filter) string {
	for _, o := range filterOptions {
		if o.Filter == f {
			return i18n.T(o.Key)
		}
	}
	return ""
}

// filterLabels returns the translated labels, for building the dropdown.
func filterLabels() []string {
	labels := make([]string, len(filterOptions))
	for i, o := range filterOptions {
		labels[i] = i18n.T(o.Key)
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
