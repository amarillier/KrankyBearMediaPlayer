// Package main - db.go provides the SQLite-backed media library catalog.
//
// We use modernc.org/sqlite (pure-Go, no CGo) so the catalog layer stays
// portable across the project's cross-compile targets. The schema tracks
// each media file once along with the two pieces of play tracking the user
// cares about: a play count and a manual star rating.
//
// PORTABILITY: tracks are stored as a path RELATIVE to their watched folder
// (forward-slash normalized), never as an absolute path. The absolute path is
// reconstructed at runtime as filepath.Join(folder.root, rel_path). That means
// when the same library moves between machines - e.g. a USB drive that mounts
// as D:\ on one PC and E:\ on another - only the folder's root needs updating
// (DB.RelocateFolder), and every track under it follows automatically. This is
// the fix for AIMP's habit of pinning absolute paths at discovery time.
//
// Effective rating = the manual 1-5 star rating if set, otherwise 0. Play count
// is tracked separately and is NOT turned into stars. See rating.go.
package main

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Track is one media file in the catalog.
type Track struct {
	ID          int64
	FolderID    int64
	FolderRoot  string // watched-folder root, populated on read; not stored here
	RelPath     string // path relative to FolderRoot, forward-slash normalized
	Title       string
	Artist      string
	Album       string
	AlbumArtist string
	Genre       string
	Year        int
	TrackNo     int
	Duration    int // seconds
	FileSize    int64
	FileMtime   int64
	HasArt      bool
	PlayCount   int
	Rating      sql.NullInt64 // manual rating 1..5, NULL = not manually set
	DateAdded   int64
	LastSeen    int64
}

// AbsPath reconstructs the on-disk path from the current folder root. This is
// where drive-letter / mount-point portability happens.
func (t Track) AbsPath() string {
	return filepath.Join(t.FolderRoot, filepath.FromSlash(t.RelPath))
}

// EffectiveRating returns the manual star rating (0 if unrated). Ratings are
// manual-only - play count is shown in its own column, not turned into stars.
func (t Track) EffectiveRating() int {
	if t.Rating.Valid {
		return int(t.Rating.Int64)
	}
	return 0
}

// DB wraps the catalog database connection.
type DB struct {
	sql *sql.DB
}

// effRatingExpr is the SQL form of Track.EffectiveRating (manual rating, 0 if
// unrated), used in WHERE/ORDER.
const effRatingExpr = "COALESCE(rating, 0)"

const schema = `
CREATE TABLE IF NOT EXISTS folders (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	path         TEXT NOT NULL UNIQUE,   -- root; remap this to relocate a library
	recursive    INTEGER NOT NULL DEFAULT 1,
	last_scanned INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS tracks (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	folder_id    INTEGER NOT NULL REFERENCES folders(id) ON DELETE CASCADE,
	rel_path     TEXT NOT NULL,          -- relative to folders.path, '/'-normalized
	title        TEXT NOT NULL DEFAULT '',
	artist       TEXT NOT NULL DEFAULT '',
	album        TEXT NOT NULL DEFAULT '',
	album_artist TEXT NOT NULL DEFAULT '',
	genre        TEXT NOT NULL DEFAULT '',
	year         INTEGER NOT NULL DEFAULT 0,
	track_no     INTEGER NOT NULL DEFAULT 0,
	duration     INTEGER NOT NULL DEFAULT 0,
	file_size    INTEGER NOT NULL DEFAULT 0,
	file_mtime   INTEGER NOT NULL DEFAULT 0,
	has_art      INTEGER NOT NULL DEFAULT 0,
	play_count   INTEGER NOT NULL DEFAULT 0,
	rating       INTEGER,                 -- NULL = no manual rating
	date_added   INTEGER NOT NULL DEFAULT 0,
	last_seen    INTEGER NOT NULL DEFAULT 0,
	UNIQUE(folder_id, rel_path)
);

-- Album-art thumbnails kept in a side table so list queries stay light.
CREATE TABLE IF NOT EXISTS thumbs (
	track_id INTEGER PRIMARY KEY REFERENCES tracks(id) ON DELETE CASCADE,
	png      BLOB NOT NULL
);

CREATE TABLE IF NOT EXISTS playlists (
	id      INTEGER PRIMARY KEY AUTOINCREMENT,
	name    TEXT NOT NULL,
	created INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS playlist_tracks (
	playlist_id INTEGER NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
	track_id    INTEGER NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
	position    INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (playlist_id, track_id)
);

-- Dynamic ("smart") playlists store filter criteria, not a fixed track list, so
-- they re-evaluate live against the catalog. Static playlists are .m3u8 files on
-- disk instead (see playlist.go); the playlists/playlist_tracks tables above are
-- reserved for a possible future in-DB static playlist.
CREATE TABLE IF NOT EXISTS smart_playlists (
	id      INTEGER PRIMARY KEY AUTOINCREMENT,
	name    TEXT NOT NULL UNIQUE,
	filter  INTEGER NOT NULL DEFAULT 0,  -- Filter enum (rating)
	search  TEXT NOT NULL DEFAULT '',
	genre   TEXT NOT NULL DEFAULT '',
	artist  TEXT NOT NULL DEFAULT '',
	album   TEXT NOT NULL DEFAULT '',
	created INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_tracks_album  ON tracks(album);
CREATE INDEX IF NOT EXISTS idx_tracks_artist ON tracks(artist);
`

// openDB opens (creating if needed) the catalog database at path.
func openDB(path string) (*DB, error) {
	sqldb, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	// modernc sqlite is fine with a single connection; serialize to avoid
	// "database is locked" under the playback goroutine writing play counts.
	sqldb.SetMaxOpenConns(1)
	if _, err := sqldb.Exec("PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;"); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("set pragmas: %w", err)
	}
	if _, err := sqldb.Exec(schema); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &DB{sql: sqldb}, nil
}

func (d *DB) Close() error { return d.sql.Close() }

// AddFolder records a watched folder, returning its id (existing or new).
func (d *DB) AddFolder(path string, recursive bool) (int64, error) {
	rec := 0
	if recursive {
		rec = 1
	}
	_, err := d.sql.Exec(
		`INSERT INTO folders(path, recursive) VALUES(?, ?)
		 ON CONFLICT(path) DO UPDATE SET recursive=excluded.recursive`,
		path, rec)
	if err != nil {
		return 0, err
	}
	var id int64
	err = d.sql.QueryRow(`SELECT id FROM folders WHERE path=?`, path).Scan(&id)
	return id, err
}

// Folder is a watched library root.
type Folder struct {
	ID        int64
	Path      string
	Recursive bool
}

// DeleteFolder removes a watched folder and, via ON DELETE CASCADE (foreign keys
// are enabled), all of its catalogued tracks and their thumbnails. The files on
// disk are untouched.
func (d *DB) DeleteFolder(id int64) error {
	_, err := d.sql.Exec(`DELETE FROM folders WHERE id=?`, id)
	return err
}

// CountTracksInFolder returns how many catalogued tracks belong to a folder
// (used to warn before removing it).
func (d *DB) CountTracksInFolder(id int64) (int, error) {
	var n int
	err := d.sql.QueryRow(`SELECT COUNT(*) FROM tracks WHERE folder_id=?`, id).Scan(&n)
	return n, err
}

// TracksInFolder returns the minimal track rows (id + rel_path) catalogued under
// one folder, used by the scan to detect orphans whose file has gone missing.
// FolderRoot is left to the caller to fill from the live folder path.
func (d *DB) TracksInFolder(folderID int64) ([]Track, error) {
	rows, err := d.sql.Query(`SELECT id, rel_path, title FROM tracks WHERE folder_id=?`, folderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Track
	for rows.Next() {
		t := Track{FolderID: folderID}
		if err := rows.Scan(&t.ID, &t.RelPath, &t.Title); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Folders returns all watched folders.
func (d *DB) Folders() ([]Folder, error) {
	rows, err := d.sql.Query(`SELECT id, path, recursive FROM folders ORDER BY path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Folder
	for rows.Next() {
		var f Folder
		var rec int
		if err := rows.Scan(&f.ID, &f.Path, &rec); err != nil {
			return nil, err
		}
		f.Recursive = rec != 0
		out = append(out, f)
	}
	return out, rows.Err()
}

// RelocateFolder changes a watched folder's root path. Because tracks store
// paths relative to this root, every track under the folder is relocated by
// this single update - the answer to "USB drive is D:\ here but E:\ there".
func (d *DB) RelocateFolder(folderID int64, newRoot string) error {
	_, err := d.sql.Exec(`UPDATE folders SET path=? WHERE id=?`, newRoot, folderID)
	return err
}

func (d *DB) SetFolderScanned(id int64) error {
	_, err := d.sql.Exec(`UPDATE folders SET last_scanned=? WHERE id=?`,
		time.Now().Unix(), id)
	return err
}

// UpsertTrack inserts or updates a track by (folder_id, rel_path). Play count
// and manual rating are preserved across re-scans (never overwritten).
func (d *DB) UpsertTrack(t *Track) error {
	now := time.Now().Unix()
	hasArt := 0
	if t.HasArt {
		hasArt = 1
	}
	res, err := d.sql.Exec(`
		INSERT INTO tracks(folder_id, rel_path, title, artist, album, album_artist,
			genre, year, track_no, duration, file_size, file_mtime, has_art,
			date_added, last_seen)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(folder_id, rel_path) DO UPDATE SET
			title=excluded.title, artist=excluded.artist, album=excluded.album,
			album_artist=excluded.album_artist, genre=excluded.genre,
			year=excluded.year, track_no=excluded.track_no,
			duration=excluded.duration, file_size=excluded.file_size,
			file_mtime=excluded.file_mtime, has_art=excluded.has_art,
			last_seen=excluded.last_seen`,
		t.FolderID, t.RelPath, t.Title, t.Artist, t.Album, t.AlbumArtist,
		t.Genre, t.Year, t.TrackNo, t.Duration, t.FileSize, t.FileMtime,
		hasArt, now, now)
	if err != nil {
		return err
	}
	if id, err := res.LastInsertId(); err == nil && id != 0 {
		t.ID = id
	}
	if t.ID == 0 {
		_ = d.sql.QueryRow(`SELECT id FROM tracks WHERE folder_id=? AND rel_path=?`,
			t.FolderID, t.RelPath).Scan(&t.ID)
	}
	return nil
}

// SetThumb stores (or replaces) a PNG album-art thumbnail for a track.
func (d *DB) SetThumb(trackID int64, png []byte) error {
	_, err := d.sql.Exec(
		`INSERT INTO thumbs(track_id, png) VALUES(?, ?)
		 ON CONFLICT(track_id) DO UPDATE SET png=excluded.png`,
		trackID, png)
	return err
}

// SetArt stores (or replaces) a track's album-art thumbnail and marks the track
// as having art. Used when the user adds cover art from a file or URL; the art
// lives in the catalog (it is not embedded into the audio file).
func (d *DB) SetArt(trackID int64, png []byte) error {
	if err := d.SetThumb(trackID, png); err != nil {
		return err
	}
	_, err := d.sql.Exec(`UPDATE tracks SET has_art=1 WHERE id=?`, trackID)
	return err
}

// UpdateTrackTags writes a track's edited metadata fields to the catalog after
// the tags have been saved to the file (see tagedit.go). It deliberately leaves
// play_count, rating, and album art untouched - those are managed separately,
// so editing tags never disturbs them.
func (d *DB) UpdateTrackTags(trackID int64, t trackTags) error {
	_, err := d.sql.Exec(`
		UPDATE tracks SET title=?, artist=?, album=?, album_artist=?,
			genre=?, year=?, track_no=? WHERE id=?`,
		t.Title, t.Artist, t.Album, t.AlbumArtist, t.Genre, t.Year, t.Track, trackID)
	return err
}

// UpdateTrackPath records a file's new location after a rename. relPath is relative
// to the track's folder root, forward-slash normalized. A clash with another track's
// path violates UNIQUE(folder_id, rel_path) and surfaces as an error.
func (d *DB) UpdateTrackPath(trackID int64, relPath string) error {
	_, err := d.sql.Exec(`UPDATE tracks SET rel_path=? WHERE id=?`, relPath, trackID)
	return err
}

// DeleteTrack removes a single track from the catalog by id. Via ON DELETE CASCADE
// (foreign keys are enabled) its thumbnail and any playlist memberships go with it.
// The file on disk is the caller's responsibility.
func (d *DB) DeleteTrack(id int64) error {
	_, err := d.sql.Exec(`DELETE FROM tracks WHERE id=?`, id)
	return err
}

// TrackRelPath returns a track's stored rel_path (forward-slash normalized). Used
// by batch rename to derive a new rel_path for marked tracks that may not be in the
// current view.
func (d *DB) TrackRelPath(trackID int64) (string, error) {
	var rel string
	err := d.sql.QueryRow(`SELECT rel_path FROM tracks WHERE id=?`, trackID).Scan(&rel)
	return rel, err
}

// TracksMissingDuration returns tracks whose playback length hasn't been
// computed yet (duration <= 0), for the background enricher (enrich.go). Only
// the fields needed to locate the file and derive bitrate are populated.
func (d *DB) TracksMissingDuration() ([]Track, error) {
	rows, err := d.sql.Query(`
		SELECT t.id, f.path, t.rel_path, t.file_size
		FROM tracks t JOIN folders f ON f.id = t.folder_id
		WHERE t.duration <= 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Track
	for rows.Next() {
		var t Track
		if err := rows.Scan(&t.ID, &t.FolderRoot, &t.RelPath, &t.FileSize); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SetDuration stores a track's playback length in whole seconds.
func (d *DB) SetDuration(trackID int64, seconds int) error {
	_, err := d.sql.Exec(`UPDATE tracks SET duration=? WHERE id=?`, seconds, trackID)
	return err
}

// ClearArt removes a track's album-art thumbnail and marks it as having none.
func (d *DB) ClearArt(trackID int64) error {
	if _, err := d.sql.Exec(`DELETE FROM thumbs WHERE track_id=?`, trackID); err != nil {
		return err
	}
	_, err := d.sql.Exec(`UPDATE tracks SET has_art=0 WHERE id=?`, trackID)
	return err
}

// Thumb returns the PNG thumbnail for a track, or nil if none.
func (d *DB) Thumb(trackID int64) []byte {
	var png []byte
	err := d.sql.QueryRow(`SELECT png FROM thumbs WHERE track_id=?`, trackID).Scan(&png)
	if err != nil {
		return nil
	}
	return png
}

// IncrementPlayCount bumps a track's play count by one (called when a track
// finishes playing). Play count is tracked on its own; it does not affect stars.
func (d *DB) IncrementPlayCount(trackID int64) error {
	_, err := d.sql.Exec(`UPDATE tracks SET play_count=play_count+1 WHERE id=?`, trackID)
	return err
}

// SetRating sets (rating 1..5) or clears (rating <= 0) the manual star rating.
func (d *DB) SetRating(trackID int64, rating int) error {
	if rating <= 0 {
		_, err := d.sql.Exec(`UPDATE tracks SET rating=NULL WHERE id=?`, trackID)
		return err
	}
	_, err := d.sql.Exec(`UPDATE tracks SET rating=? WHERE id=?`, rating, trackID)
	return err
}

// TrackQuery selects which tracks to return and how to order them.
type TrackQuery struct {
	Filter Filter // rating filter
	Search string // case-insensitive substring across title/artist/album/genre/filename
	// Exact-match (case-insensitive) constraints used by smart playlists; empty
	// means "no constraint". Combined with Filter/Search via AND.
	Genre     string
	Artist    string
	Album     string
	// Per-column live filters (the filter row). The text fields match a
	// case-insensitive substring; FYear is a GLOB pattern against the year text,
	// so "202[456]" matches 2024-2026 and "20*" matches the 2000s. Empty = no
	// constraint. ANDed with everything else.
	FTitle  string
	FArtist string
	FAlbum  string
	FGenre  string
	FYear   string
	FPlays  string // GLOB pattern against play count text ("5" = exactly 5, "0" = unplayed)
	SortCol   int  // primary sort: a col* constant; -1 = default order
	Desc      bool // primary descending
	Sort2Col  int  // secondary sort (shift-click); -1 = none
	Sort2Desc bool // secondary descending
}

// sortExpr returns the ORDER BY term for one column (no tie-breaker tail).
// effRatingExpr lives above.
func sortExpr(col int, desc bool) string {
	dir := "ASC"
	if desc {
		dir = "DESC"
	}
	switch col {
	case colTrack:
		return "t.track_no " + dir
	case colFilename:
		return "t.rel_path " + dir
	case colTitle:
		return "t.title " + dir
	case colArtist:
		return "t.artist " + dir
	case colAlbum:
		return "t.album " + dir
	case colYear:
		return "t.year " + dir
	case colPlays:
		return "t.play_count " + dir
	case colRating:
		return effRatingExpr + " " + dir
	default:
		return "t.artist " + dir
	}
}

// orderBy builds the full SQL ORDER BY body: primary, optional secondary
// (shift-click) key, then a stable tie-breaker tail. effRatingExpr lives above.
func orderBy(q TrackQuery) string {
	if q.SortCol < 0 {
		return "t.artist, t.album, t.track_no, t.title" // default grouping
	}
	terms := sortExpr(q.SortCol, q.Desc)
	if q.Sort2Col >= 0 && q.Sort2Col != q.SortCol {
		terms += ", " + sortExpr(q.Sort2Col, q.Sort2Desc)
	}
	return terms + ", t.artist, t.album, t.track_no, t.title" // stable tie-breaker
}

// likeContains builds a case-insensitive "contains" LIKE pattern, escaping the
// SQL wildcards (% _ \) in s so they match literally. Pair with ESCAPE '\'.
func likeContains(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(s) + "%"
}

// Tracks returns catalog tracks matching the query, joined to their folder root
// so AbsPath works.
func (d *DB) Tracks(q TrackQuery) ([]Track, error) {
	sql := `SELECT t.id, t.folder_id, f.path, t.rel_path, t.title, t.artist,
			t.album, t.album_artist, t.genre, t.year, t.track_no, t.duration,
			t.file_size, t.file_mtime, t.has_art, t.play_count, t.rating,
			t.date_added, t.last_seen
		FROM tracks t JOIN folders f ON f.id = t.folder_id`

	var conds []string
	var args []any
	if where := q.Filter.where(); where != "" {
		conds = append(conds, where)
	}
	if s := strings.TrimSpace(q.Search); s != "" {
		conds = append(conds,
			`(t.title LIKE ? OR t.artist LIKE ? OR t.album LIKE ? OR t.genre LIKE ? OR t.rel_path LIKE ?)`)
		like := "%" + s + "%"
		args = append(args, like, like, like, like, like)
	}
	// Partial, case-insensitive smart-playlist constraints (substring match, so
	// "Wickham" matches "Phil Wickham"). LIKE is case-insensitive for ASCII in
	// SQLite; wildcards in the user's text are escaped.
	for _, c := range []struct {
		col, val string
	}{
		{"t.genre", q.Genre},
		{"t.artist", q.Artist},
		{"t.album", q.Album},
		// Per-column filter row (substring match on text columns).
		{"t.title", q.FTitle},
		{"t.artist", q.FArtist},
		{"t.album", q.FAlbum},
		{"t.genre", q.FGenre},
	} {
		if v := strings.TrimSpace(c.val); v != "" {
			conds = append(conds, c.col+` LIKE ? ESCAPE '\'`)
			args = append(args, likeContains(v))
		}
	}
	// Year + Plays column filters: GLOB patterns against the numeric text
	// (e.g. year "202[456]", plays "5" for exactly five, "0" for unplayed).
	if v := strings.TrimSpace(q.FYear); v != "" {
		conds = append(conds, `CAST(t.year AS TEXT) GLOB ?`)
		args = append(args, v)
	}
	if v := strings.TrimSpace(q.FPlays); v != "" {
		conds = append(conds, `CAST(t.play_count AS TEXT) GLOB ?`)
		args = append(args, v)
	}
	if len(conds) > 0 {
		sql += " WHERE " + strings.Join(conds, " AND ")
	}
	sql += " ORDER BY " + orderBy(q)

	rows, err := d.sql.Query(sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Track
	for rows.Next() {
		var t Track
		var hasArt int
		if err := rows.Scan(&t.ID, &t.FolderID, &t.FolderRoot, &t.RelPath,
			&t.Title, &t.Artist, &t.Album, &t.AlbumArtist, &t.Genre, &t.Year,
			&t.TrackNo, &t.Duration, &t.FileSize, &t.FileMtime, &hasArt,
			&t.PlayCount, &t.Rating, &t.DateAdded, &t.LastSeen); err != nil {
			return nil, err
		}
		t.HasArt = hasArt != 0
		out = append(out, t)
	}
	return out, rows.Err()
}

// SmartPlaylist is a named, saved set of filter criteria that re-evaluates live
// against the catalog (a dynamic playlist). Empty string fields mean "no
// constraint". It maps directly onto the relevant TrackQuery fields.
type SmartPlaylist struct {
	ID     int64
	Name   string
	Filter Filter
	Search string
	Genre  string
	Artist string
	Album  string
}

// Query returns the TrackQuery (default sort) that this smart playlist selects.
func (s SmartPlaylist) Query() TrackQuery {
	return TrackQuery{
		Filter: s.Filter, Search: s.Search,
		Genre: s.Genre, Artist: s.Artist, Album: s.Album,
		SortCol: -1, Sort2Col: -1,
	}
}

// SaveSmartPlaylist inserts a smart playlist, or replaces the criteria of an
// existing one with the same name (names are unique).
func (d *DB) SaveSmartPlaylist(s SmartPlaylist) error {
	_, err := d.sql.Exec(`
		INSERT INTO smart_playlists (name, filter, search, genre, artist, album, created)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			filter=excluded.filter, search=excluded.search, genre=excluded.genre,
			artist=excluded.artist, album=excluded.album`,
		s.Name, int(s.Filter), s.Search, s.Genre, s.Artist, s.Album, time.Now().Unix())
	return err
}

// UpdateSmartPlaylist replaces an existing smart playlist's name and criteria by
// id (so editing can rename, unlike the name-keyed upsert in SaveSmartPlaylist).
// Renaming to a name already in use fails on the unique-name constraint.
func (d *DB) UpdateSmartPlaylist(s SmartPlaylist) error {
	_, err := d.sql.Exec(`
		UPDATE smart_playlists SET name=?, filter=?, search=?, genre=?, artist=?, album=?
		WHERE id=?`,
		s.Name, int(s.Filter), s.Search, s.Genre, s.Artist, s.Album, s.ID)
	return err
}

// SmartPlaylists returns all saved smart playlists, ordered by name.
func (d *DB) SmartPlaylists() ([]SmartPlaylist, error) {
	rows, err := d.sql.Query(`SELECT id, name, filter, search, genre, artist, album
		FROM smart_playlists ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SmartPlaylist
	for rows.Next() {
		var s SmartPlaylist
		var f int
		if err := rows.Scan(&s.ID, &s.Name, &f, &s.Search, &s.Genre, &s.Artist, &s.Album); err != nil {
			return nil, err
		}
		s.Filter = Filter(f)
		out = append(out, s)
	}
	return out, rows.Err()
}

// DeleteSmartPlaylist removes a smart playlist by id.
func (d *DB) DeleteSmartPlaylist(id int64) error {
	_, err := d.sql.Exec(`DELETE FROM smart_playlists WHERE id = ?`, id)
	return err
}
