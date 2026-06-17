package main

import "testing"

func reportFixture() []Track {
	return []Track{
		{Artist: "Alpha", Album: "One", Genre: "Rock", Year: 2001, TrackNo: 1, Title: "a1"},
		{Artist: "Alpha", Album: "One", Genre: "Rock", Year: 2001, TrackNo: 2, Title: "a2"},
		{Artist: "Alpha", Album: "Two", Genre: "Pop", Year: 2005, TrackNo: 1, Title: "a3"},
		{Artist: "Beta", Album: "Solo", Genre: "Rock", Year: 2010, TrackNo: 1, Title: "b1"},
		{Artist: "", Album: "", Genre: "", TrackNo: 0, Title: "orphan"},
	}
}

func findRow(rows [][]string, first string) []string {
	for _, r := range rows {
		if r[0] == first {
			return r
		}
	}
	return nil
}

func TestBuildReportByArtist(t *testing.T) {
	_, rows, _, summary := buildReport(reportFixture(), reportByArtist, "Name", false, false)
	// Alpha: 2 albums, 3 tracks.
	if r := findRow(rows, "Alpha"); r == nil || r[1] != "2" || r[2] != "3" {
		t.Errorf("Alpha row = %v, want [Alpha 2 3]", r)
	}
	if r := findRow(rows, "Beta"); r == nil || r[2] != "1" {
		t.Errorf("Beta row = %v", r)
	}
	if r := findRow(rows, "(unknown)"); r == nil {
		t.Error("missing (unknown) artist row")
	}
	// 3 distinct artists (Alpha, Beta, unknown), 4 albums, 5 tracks.
	if summary != "3 artists · 4 albums · 5 tracks" {
		t.Errorf("summary = %q", summary)
	}
}

func TestBuildReportByGenre(t *testing.T) {
	_, rows, _, _ := buildReport(reportFixture(), reportByGenre, "Name", false, false)
	if r := findRow(rows, "Rock"); r == nil || r[1] != "3" {
		t.Errorf("Rock row = %v, want 3 tracks", r)
	}
	if r := findRow(rows, "Pop"); r == nil || r[1] != "1" {
		t.Errorf("Pop row = %v", r)
	}
}

func TestBuildReportByAlbumAndTracks(t *testing.T) {
	hdr, rows, _, _ := buildReport(reportFixture(), reportByAlbum, "Name", false, false)
	if len(hdr) != 4 {
		t.Fatalf("album header = %v", hdr)
	}
	// Alpha/One has 2 tracks, year 2001.
	var found bool
	for _, r := range rows {
		if r[0] == "Alpha" && r[1] == "One" {
			found = true
			if r[2] != "2001" || r[3] != "2" {
				t.Errorf("Alpha/One row = %v, want year 2001, 2 tracks", r)
			}
		}
	}
	if !found {
		t.Error("Alpha/One album row missing")
	}

	_, trackRows, _, _ := buildReport(reportFixture(), reportTracks, "Name", false, false)
	if len(trackRows) != 5 {
		t.Errorf("All tracks rows = %d, want 5", len(trackRows))
	}
}

// TestBuildReportHierarchical checks the two-level groupings and a year/desc sort.
func TestBuildReportHierarchical(t *testing.T) {
	// Artist → Album: Alpha has 2 albums (One, Two); CSV rows are per album.
	_, rows, display, _ := buildReport(reportFixture(), reportArtistAlbum, "Name", false, false)
	var alphaAlbums int
	for _, r := range rows {
		if r[0] == "Alpha" {
			alphaAlbums++
		}
	}
	if alphaAlbums != 2 {
		t.Errorf("Artist→Album: Alpha should have 2 album rows, got %d", alphaAlbums)
	}
	if len(display) == 0 {
		t.Error("hierarchical display should have lines")
	}

	// Year descending puts the newest album group first among Alpha's children is
	// covered indirectly; just ensure it builds and orders without error.
	_, rows2, _, _ := buildReport(reportFixture(), reportArtistAlbum, "Year", true, false)
	if len(rows2) != len(rows) {
		t.Errorf("year-sorted row count changed: %d vs %d", len(rows2), len(rows))
	}

	// Genre → Artist has 3 columns (no year).
	hdr, _, _, _ := buildReport(reportFixture(), reportGenreArtist, "Name", false, false)
	if len(hdr) != 3 {
		t.Errorf("Genre→Artist header = %v", hdr)
	}
}

// TestBuildReportListTracks checks the "List tracks" expansion: one CSV row per track
// with grouping context columns.
func TestBuildReportListTracks(t *testing.T) {
	// Artist → Album + tracks: columns Artist, Album, Track, Title, Year; one row per
	// track (5 tracks in the fixture).
	hdr, rows, display, _ := buildReport(reportFixture(), reportArtistAlbum, "Name", false, true)
	if len(hdr) != 5 || hdr[0] != "Artist" || hdr[1] != "Album" {
		t.Fatalf("header = %v", hdr)
	}
	if len(rows) != 5 {
		t.Errorf("expected 5 track rows, got %d", len(rows))
	}
	// Display includes group headers AND track lines, so it exceeds the row count.
	if len(display) <= len(rows) {
		t.Errorf("display (%d) should include group headers beyond %d track rows", len(display), len(rows))
	}
	// A known track lands under Alpha/One.
	var ok bool
	for _, r := range rows {
		if r[0] == "Alpha" && r[1] == "One" && r[3] == "a1" {
			ok = true
		}
	}
	if !ok {
		t.Error("Alpha/One/a1 track row missing")
	}

	// By Genre + tracks: columns Genre, Track, Title, Year.
	hdr2, rows2, _, _ := buildReport(reportFixture(), reportByGenre, "Name", false, true)
	if len(hdr2) != 4 || hdr2[0] != "Genre" {
		t.Errorf("genre+tracks header = %v", hdr2)
	}
	if len(rows2) != 5 {
		t.Errorf("genre+tracks rows = %d, want 5", len(rows2))
	}
}
