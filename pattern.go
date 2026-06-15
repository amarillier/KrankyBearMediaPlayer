// Package main - pattern.go is the shared filename<->tags pattern grammar used by
// the two paired tools: rename-from-tags (rename.go) builds a filename from a
// track's tags, and tags-from-filename (the editor's "Tags from filename…" action)
// parses a filename back into tag fields. ONE token grammar, run in two directions
// (build vs. parse), so the same pattern works both ways.
//
// Tokens (case-insensitive): %title% %artist% %album% %albumartist% %track%
// %year% %genre%. %track% is zero-padded to two digits when building.
package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// tokenRe matches a %token% in a pattern (case-insensitive). The submatch is the
// bare token name without the percent signs.
var tokenRe = regexp.MustCompile(`(?i)%([a-z]+)%`)

// renamePresets are the built-in pattern choices offered in the rename / parse
// dialogs (in addition to the user's saved last-used pattern).
var renamePresets = []string{
	"%track% %title%",
	"%track% %title% - %artist%",
	"%artist% - %title%",
	"%album% - %track% - %title%",
}

// buildName turns a pattern into a filename body (no extension) by substituting
// the track's tags for each known token. %track%/%year% expand to digits (track
// zero-padded to two places, both empty when the value is 0); unknown tokens are
// left verbatim so a typo is visible in the preview. The result is run through
// sanitizeBaseName so it's always a legal, separator-free base name.
func buildName(pattern string, tt trackTags) string {
	out := tokenRe.ReplaceAllStringFunc(pattern, func(tok string) string {
		switch strings.ToLower(tok) {
		case "%title%":
			return tt.Title
		case "%artist%":
			return tt.Artist
		case "%album%":
			return tt.Album
		case "%albumartist%":
			return tt.AlbumArtist
		case "%genre%":
			return tt.Genre
		case "%year%":
			if tt.Year > 0 {
				return strconv.Itoa(tt.Year)
			}
			return ""
		case "%track%":
			if tt.Track > 0 {
				return fmt.Sprintf("%02d", tt.Track)
			}
			return ""
		}
		return tok // unknown token: leave literal so the typo shows in preview
	})
	return sanitizeBaseName(out)
}

// parseName runs the grammar in reverse: it compiles the pattern into an anchored
// regex and extracts tag fields from a filename stem (extension already stripped).
// It returns a map keyed by canonical field name ("title", "artist", "album",
// "albumartist", "genre", "track", "year") holding only the fields the pattern
// actually captured, and ok=false when the filename doesn't match the pattern (so
// the caller overwrites nothing). hasLeadingTrack prepends an optional leading
// track-number capture when the pattern itself doesn't already start with %track%
// (covers names like "01 Title" parsed with a pattern of just "%title%").
func parseName(pattern, stem string, hasLeadingTrack bool) (map[string]string, bool) {
	re, fields, err := compilePattern(pattern, hasLeadingTrack)
	if err != nil {
		return nil, false
	}
	m := re.FindStringSubmatch(strings.TrimSpace(stem))
	if m == nil {
		return nil, false
	}
	out := make(map[string]string, len(fields))
	for i, f := range fields {
		v := strings.TrimSpace(m[i+1])
		if v == "" {
			continue
		}
		if _, dup := out[f]; dup {
			continue // first capture wins if a token repeats
		}
		out[f] = v
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// compilePattern builds the parse regex for a pattern. Literal text between tokens
// is escaped; numeric tokens (track/year) capture digits and text tokens capture a
// non-greedy run. fields lists the canonical field name for each capture group in
// order. Unknown %tokens% are treated as literal text.
func compilePattern(pattern string, hasLeadingTrack bool) (*regexp.Regexp, []string, error) {
	locs := tokenRe.FindAllStringSubmatchIndex(pattern, -1)

	var b strings.Builder
	var fields []string
	b.WriteString("^")

	firstIsTrack := len(locs) > 0 &&
		strings.EqualFold(pattern[locs[0][2]:locs[0][3]], "track")
	if hasLeadingTrack && !firstIsTrack {
		b.WriteString(`(\d+)[\s._-]*`)
		fields = append(fields, "track")
	}

	last := 0
	for _, m := range locs {
		b.WriteString(regexp.QuoteMeta(pattern[last:m[0]])) // literal before token
		name := strings.ToLower(pattern[m[2]:m[3]])
		switch name {
		case "track", "year":
			b.WriteString(`(\d+)`)
			fields = append(fields, name)
		case "title", "artist", "album", "albumartist", "genre":
			b.WriteString(`(.+?)`)
			fields = append(fields, name)
		default:
			b.WriteString(regexp.QuoteMeta(pattern[m[0]:m[1]])) // unknown: literal
		}
		last = m[1]
	}
	b.WriteString(regexp.QuoteMeta(pattern[last:]))
	b.WriteString("$")

	re, err := regexp.Compile(b.String())
	return re, fields, err
}

// sanitizeBaseName makes a string safe as a filename base (no extension): it
// replaces path separators and Windows-illegal characters with spaces, collapses
// runs of whitespace, and trims leading/trailing spaces and separator punctuation.
// Because separators become spaces, a pattern can never create subfolders - renames
// always stay in the file's current directory. Mirrors sanitizeFolderName.
func sanitizeBaseName(s string) string {
	s = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return ' '
		}
		if r < 0x20 {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ") // collapse whitespace
	return strings.Trim(s, " .-_")
}
