package main

import (
	"errors"
	"os"
	"strings"

	updatechecker "github.com/amarillier/go-update-checker"
	"github.com/hashicorp/go-version"
)

func checkFileExists(filePath string) bool {
	_, error := os.Stat(filePath)
	return !errors.Is(error, os.ErrNotExist)
}

// updateChecker queries GitHub for the latest release. It returns the display
// message, whether a newer release is available, and the latest release tag
// (RemoteTag) so callers can tell when the local build is *ahead* of the
// published release.
func updateChecker(repoOwner string, repo string, repoName string, repodl string, checkStatePath string) (msg string, updateAvailable bool, remoteTag string) {
	if checkStatePath != "" {
		updatechecker.SetCheckStatePath(checkStatePath)
	}
	uc := updatechecker.New(repoOwner, repo, repoName, repodl, 0, false)
	uc.CheckForUpdate(appVersion)
	return uc.Message, uc.UpdateAvailable, uc.RemoteTag
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