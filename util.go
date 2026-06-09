package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	updatechecker "github.com/amarillier/go-update-checker"
	"github.com/hashicorp/go-version"
)

// Update-check cache + throttle. The cache (last-seen release tag/date) lives
// beside the app's other per-user data; a repo-specific filename avoids clashing
// with another app's generic latestcheck.json. The automatic launch check runs
// at most once per day; the manual Help/tray check is never throttled.
const (
	updateRepoOwner          = "amarillier"
	updateRepoName           = "KrankyBearMediaPlayer"
	updateCheckStateFileName = "latestcheck-KrankyBearMediaPlayer.json"
	launchUpdateIntervalDays = 1
)

func checkFileExists(filePath string) bool {
	_, error := os.Stat(filePath)
	return !errors.Is(error, os.ErrNotExist)
}

// updateCheckStatePath returns where go-update-checker should cache its result,
// under os.UserConfigDir()/KrankyBearMediaPlayer/ (the same area as Fyne
// preferences). Returns "" if the dir can't be resolved/created, in which case
// the library falls back to its own default location.
func updateCheckStatePath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return ""
	}
	appDir := filepath.Join(dir, "KrankyBearMediaPlayer")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return ""
	}
	return filepath.Join(appDir, updateCheckStateFileName)
}

// updateChecker queries GitHub for the latest release. minDays is the minimum
// days between network checks (0 = always check; 1 = at most once/day, using the
// cached result in between). Returns the display message, whether a newer release
// is available, and the latest release tag (so callers can tell when the local
// build is *ahead* of the published release).
func updateChecker(repoOwner string, repo string, repoName string, repodl string, checkStatePath string, minDays int) (msg string, updateAvailable bool, remoteTag string) {
	if checkStatePath != "" {
		updatechecker.SetCheckStatePath(checkStatePath)
	}
	uc := updatechecker.New(repoOwner, repo, repoName, repodl, minDays, false)
	uc.CheckForUpdate(appVersion)
	return uc.Message, uc.UpdateAvailable, uc.RemoteTag
}

// prefLastUpdateNotify remembers the last auto-notification we showed, as
// "YYYY-MM-DD|remoteTag", so the launch dialog appears at most once per day per
// release instead of on every launch.
const prefLastUpdateNotify = "lastUpdateNotify"

// checkForUpdatesAuto runs the launch check on a background goroutine. It shows
// the dialog when a newer release is available OR when this build is newer than
// the latest release ("ahead" -> HardHat bear), but AT MOST ONCE PER DAY per
// release (so repeated launches/test builds don't nag), and never when exactly
// up to date. The library's minDaysInterval throttles the GitHub call; this
// throttles the popup. Requires a public repo with a published Release, else the
// API 404s and nothing shows. The manual "Check for Updates" is never throttled.
func checkForUpdatesAuto(a fyne.App) {
	go func() {
		msg, avail, remoteTag := updateChecker(updateRepoOwner, updateRepoName,
			updateRepoName, "", updateCheckStatePath(), launchUpdateIntervalDays)
		ahead := versionIsNewer(appVersion, remoteTag)
		if !avail && !ahead {
			return // exactly up to date (or release unreadable): stay quiet
		}
		// Show at most once per day per release.
		sig := time.Now().Format("2006-01-02") + "|" + strings.TrimSpace(remoteTag)
		if a.Preferences().String(prefLastUpdateNotify) == sig {
			return
		}
		a.Preferences().SetString(prefLastUpdateNotify, sig)
		fyne.Do(func() { showUpdateDialog(a, msg, avail, ahead) })
	}()
}

// versionIsNewer reports whether local is a strictly newer semantic version
// than remote. Returns false if remote is empty (e.g. offline) or either side
// fails to parse - in those cases we treat the build as "not ahead".
func versionIsNewer(local, remote string) bool {
	if strings.TrimSpace(remote) == "" {
		return false
	}
	lv, errL := version.NewVersion(strings.TrimPrefix(strings.TrimSpace(local), "v"))
	rv, errR := version.NewVersion(strings.TrimPrefix(strings.TrimSpace(remote), "v"))
	if errL != nil || errR != nil {
		return false
	}
	return lv.GreaterThan(rv)
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942