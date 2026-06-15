package main

import (
	"reflect"
	"testing"
)

func TestBuildName(t *testing.T) {
	full := trackTags{
		Title: "Sound of Silence", Artist: "Simon & Garfunkel",
		Album: "Wednesday Morning", AlbumArtist: "S&G", Genre: "Folk",
		Year: 1964, Track: 1,
	}
	tests := []struct {
		name    string
		pattern string
		tt      trackTags
		want    string
	}{
		{"track zero-padded", "%track% %title%", full, "01 Sound of Silence"},
		{"track + title + artist", "%track% %title% - %artist%", full,
			"01 Sound of Silence - Simon & Garfunkel"},
		{"album and year", "%album% (%year%)", full, "Wednesday Morning (1964)"},
		{"missing track drops to nothing, trimmed", "%track% %title%",
			trackTags{Title: "Solo"}, "Solo"},
		{"missing year leaves trimmed body", "%title% %year%",
			trackTags{Title: "Solo"}, "Solo"},
		{"illegal chars sanitized + whitespace collapsed", "%artist%",
			trackTags{Artist: "AC/DC: Live?"}, "AC DC Live"},
		{"unknown token left literal", "%title% %bogus%",
			trackTags{Title: "X"}, "X %bogus%"},
		{"extension never added by grammar", "%title%",
			trackTags{Title: "song.mp3"}, "song.mp3"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildName(tc.pattern, tc.tt); got != tc.want {
				t.Errorf("buildName(%q) = %q, want %q", tc.pattern, got, tc.want)
			}
		})
	}
}

func TestParseName(t *testing.T) {
	tests := []struct {
		name        string
		pattern     string
		stem        string
		leadingNum  bool
		wantOK      bool
		wantFields  map[string]string
	}{
		{
			name:    "FUTURE.md example",
			pattern: "%track% %title% - %artist%",
			stem:    "01 Sound of Silence - Simon & Garfunkel",
			wantOK:  true,
			wantFields: map[string]string{
				"track": "01", "title": "Sound of Silence", "artist": "Simon & Garfunkel",
			},
		},
		{
			name:       "leading track toggle on pattern without %track%",
			pattern:    "%title%",
			stem:       "01 Solo",
			leadingNum: true,
			wantOK:     true,
			wantFields: map[string]string{"track": "01", "title": "Solo"},
		},
		{
			name:    "no match returns false",
			pattern: "%track% - %title%",
			stem:    "no separator here",
			wantOK:  false,
		},
		{
			name:       "leading track off keeps number in title",
			pattern:    "%title%",
			stem:       "01 Solo",
			leadingNum: false,
			wantOK:     true,
			wantFields: map[string]string{"title": "01 Solo"},
		},
		{
			name:    "only-present fields reported",
			pattern: "%artist% - %album%",
			stem:    "Band - Record",
			wantOK:  true,
			wantFields: map[string]string{"artist": "Band", "album": "Record"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseName(tc.pattern, tc.stem, tc.leadingNum)
			if ok != tc.wantOK {
				t.Fatalf("parseName ok = %v, want %v (got %v)", ok, tc.wantOK, got)
			}
			if !tc.wantOK {
				return
			}
			if !reflect.DeepEqual(got, tc.wantFields) {
				t.Errorf("parseName = %v, want %v", got, tc.wantFields)
			}
		})
	}
}

func TestParseRoundTripsBuild(t *testing.T) {
	pattern := "%track% %title% - %artist%"
	tt := trackTags{Title: "My Song", Artist: "The Band", Track: 7}
	built := buildName(pattern, tt) // "07 My Song - The Band"
	got, ok := parseName(pattern, built, false)
	if !ok {
		t.Fatalf("parseName failed on built name %q", built)
	}
	want := map[string]string{"track": "07", "title": "My Song", "artist": "The Band"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip = %v, want %v", got, want)
	}
}

func TestSanitizeBaseName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"a/b\\c:d", "a b c d"},
		{`bad*?"<>|name`, "bad name"},
		{"  spaced   out  ", "spaced out"},
		{"- trailing dash -", "trailing dash"},
		{"normal name", "normal name"},
	}
	for _, tc := range tests {
		if got := sanitizeBaseName(tc.in); got != tc.want {
			t.Errorf("sanitizeBaseName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
