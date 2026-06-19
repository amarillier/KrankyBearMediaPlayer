# KrankyBear MediaPlayer

A cross-platform music **library manager & player** built with Go and the [Fyne](https://fyne.io/)
GUI toolkit — a small, fast, trustworthy alternative for cataloguing, tagging, and playing
your music collection. Inspired by the spreadsheet-style library and play-tracking of
players like AIMP, with the tag- and file-management depth of tools like MusicBee.

Design philosophy aligns with Fyne: ease of use, solid functionality, steady bug
fixing and performance work. Some areas are a little slow; improving them is ongoing.

## Features

- **SQLite media catalog** across multiple watched folders (pure-Go `modernc.org/sqlite`
  driver — no CGo for the database).
- **Spreadsheet-style track table** with embedded album-art thumbnails; sort by any
  column (click the header, click again to reverse).
- **Play tracking**: a play count plus a manual 1–5 star rating per track. Rate by
  clicking the in-row stars, the "Rate:" bar, or the right-click menu. Play count is
  tracked and shown in its own column.
- **Configurable play counting**: a play is credited after a chosen percentage of the
  track (25/50/75/90% or end of track); skipping never counts.
- **Rating filters**: All, Unrated, exact stars, or N-stars-and-up.
- **Live search** by title, artist, album, genre, or filename, plus an optional
  **per-column filter row** (substring on text columns; patterns on Year / Plays).
- **Playlists**: portable static `.m3u8` playlists (paths relative so playlist + music
  travel together) and **smart playlists** that re-evaluate saved filter criteria live.
- **Tag editing & file tools**: edit a track's tags and embedded cover (MP3/FLAC
  writable; OGG/WAV read-only), or edit a whole marked set at once. Build filenames
  from tags with a token pattern (**Rename file…**, single or batch, with preview),
  or the inverse — fill tag fields **from the filename**. The batch editor can also
  set tags per-file from each name, set fixed fields across the selection, and apply
  or remove one cover image for all.
- **Playback** of MP3, FLAC, WAV, and OGG (via [gopxl/beep](https://github.com/gopxl/beep)):
  double-click to play, transport controls, seek bar with elapsed/total time, app and
  system volume sliders, and a **stereo balance** control for asymmetric speaker setups.
- **10-band graphic equalizer** (31 Hz – 16 kHz, ±12 dB) with built-in presets and
  saveable custom presets; live, and transparent when disabled.
- **Portable library**: track paths are stored relative to their watched folder, so a
  USB drive that mounts as `D:` on one machine and `E:` on another can be relocated
  with a single **Library → Relocate Folder** — play counts and ratings come along.
  The database location is configurable (`-db` flag or `KBMP_DB` env var) and defaults
  next to the app for portable use.
- **System tray** + main menu (mirrored), **hide-all / show-all** windows (pauses
  audio), and **window-size persistence**.
- **ReplayGain** volume normalization (optional) and **gapless / crossfade** track
  transitions (experimental).
- **Keyboard shortcuts**: `Alt+P` play/pause, `Alt+H` hide all windows, `Alt+←/→`
  previous/next track, `Alt+R` Preferences.
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

Internationalisation (i18n) is the next milestone; "copy to device" sync for Android
is parked. See `ReleaseNotes.txt` for the version history and `docs/FUTURE.md` for
proposals.

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
