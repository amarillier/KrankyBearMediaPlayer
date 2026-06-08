# KrankyBear MediaPlayer

A cross-platform music library and player built with Go and the [Fyne](https://fyne.io/)
GUI toolkit — a small, fast, trustworthy alternative for cataloguing and playing your
music collection. Inspired by the spreadsheet-style library and play-tracking of
players like AIMP.

Design philosophy aligns with Fyne: ease of use, solid functionality, steady bug
fixing and performance work. Some areas are a little slow; improving them is ongoing.

## Features

- **SQLite media catalog** across multiple watched folders (pure-Go `modernc.org/sqlite`
  driver — no CGo for the database).
- **Spreadsheet-style track table** with embedded album-art thumbnails; sort by any
  column (click the header, click again to reverse).
- **Play tracking**: a play count plus a 1–5 star rating per track (empty = unplayed).
  Rate manually by clicking the in-row stars, the "Rate:" bar, or the right-click
  menu — or let an **auto rating** be derived from play count (manual ratings are
  never overwritten).
- **Configurable play counting**: a play is credited after a chosen percentage of the
  track (25/50/75/90% or end of track); skipping never counts.
- **Rating filters**: All, Unplayed, exact stars, or N-stars-and-up.
- **Live search** by title, artist, album, genre, or filename.
- **Playback** of MP3, FLAC, WAV, and OGG (via [gopxl/beep](https://github.com/gopxl/beep)):
  double-click to play, transport controls, seek bar with elapsed/total time, and a
  volume slider.
- **Portable library**: track paths are stored relative to their watched folder, so a
  USB drive that mounts as `D:` on one machine and `E:` on another can be relocated
  with a single **Library → Relocate Folder** — play counts and ratings come along.
  The database location is configurable (`-db` flag or `KBMP_DB` env var) and defaults
  next to the app for portable use.
- **System tray** + main menu (mirrored), **hide-all / show-all** windows (pauses
  audio), and **window-size persistence**.
- **Keyboard shortcuts**: `Alt+P` play/pause, `Alt+H` hide all windows.
- **Light / Dark / System** themes.

### Cross-platform support

- **Linux**: GNOME, KDE, XFCE, Cinnamon, MATE, etc. on X11 or Wayland (`.deb`/`.rpm`
  install an application-menu launcher and icon).
- **macOS**: 10.13 (High Sierra) or later.
- **Windows**: Windows 10 or later.

## Building & running

Requires Go and a Fyne-capable toolchain (CGo + OpenGL on desktop). From the project
directory:

```
go run .                 # run in place
go build -o KrankyBearMediaPlayer .
```

Platform build/package helpers are included: `compile-mac.sh`, `compile-win.sh`,
`compile-linux.sh`, and `package.sh` (builds `.deb`/`.rpm`, macOS `.pkg`).

To start a brand-new app from this template, use `rename-app.sh "New App Name" "Icon.png"`.

## Configuration

- `-db <path>` or `KBMP_DB=<path>` — choose where the library database lives. By
  default it sits next to the executable (ideal on a portable drive).

## Roadmap

Playlists, album art from disk, multi-column sort, internationalisation (i18n), and
optional music-video playback. See `ReleaseNotes.txt`.

## Supported audio formats

MP3, FLAC, WAV, OGG. (M4A/AAC/ALAC are not yet supported.)

## License

Free for personal, educational and commercial use, under the GNU GPL-3.0.

## Author

Allan Marillier

## Acknowledgments

- Built with [Fyne](https://fyne.io/) — an easy-to-use GUI toolkit for Go.
- Audio playback via [gopxl/beep](https://github.com/gopxl/beep); tag/album-art
  reading via [dhowden/tag](https://github.com/dhowden/tag).
