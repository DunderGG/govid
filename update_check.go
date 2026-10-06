// update_check.go — Checks for a newer yt-dlp than the one installed.
//
// Responsibilities:
//   - startUpdateChecks: the background check run at startup when "Check
//     for updates on startup" is enabled. An outdated yt-dlp is logged and
//     shown as a notice whose "Update now" button runs the yt-dlp updater.
//   - ytDlpVersions: the installed and latest yt-dlp versions, for the
//     Update yt-dlp dialog and the About window.
package main

import (
	"context"
	"fmt"
	"strings"
)

// The GitHub repository yt-dlp is released from.
const (
	ytDlpOwner = "yt-dlp"
	ytDlpRepo  = "yt-dlp"
)

// ytDlpNoticeID identifies the "yt-dlp is out of date" notice.
const ytDlpNoticeID = "yt-dlp-update"

// unknownVersion is shown when a version cannot be determined.
const unknownVersion = "unknown"

// ytDlpUpdateHint is logged after a download fails with an error that
// usually means the site changed (see extractorErrPatterns).
const ytDlpUpdateHint = "[SYSTEM] Hint: errors like this usually mean the site has changed. Updating yt-dlp (Tools → Update yt-dlp) often fixes them."

// startUpdateChecks runs the startup update checks in the background, unless
// enabled is false.
func (app *DownloaderApp) startUpdateChecks(enabled bool) {
	if !enabled {
		return
	}
	go app.checkYtDlpUpdate(context.Background())
}

// checkYtDlpUpdate compares the installed yt-dlp with the latest release
// (asking GitHub at most once a day). When it is out of date, it logs one
// line and shows a notice offering to update it. A check that cannot be
// completed is recorded in the log file only, since it needs no action.
func (app *DownloaderApp) checkYtDlpUpdate(ctx context.Context) {
	installed, err := app.installedYtDlpVersion()
	if err != nil {
		app.logSvc.WriteToFile(fmt.Sprintf("[SYSTEM] yt-dlp update check skipped: %v", err))
		return
	}
	release, err := app.releaseSvc.Latest(ctx, ytDlpOwner, ytDlpRepo, releaseCheckInterval)
	if err != nil {
		app.logSvc.WriteToFile(fmt.Sprintf("[SYSTEM] yt-dlp update check skipped: %v", err))
		return
	}
	if !isOlderVersion(installed, release.TagName) {
		app.logSvc.WriteToFile(fmt.Sprintf("[SYSTEM] yt-dlp %s is up to date.", installed))
		return
	}

	app.appendOutput(fmt.Sprintf("[SYSTEM] yt-dlp %s is out of date; the latest version is %s.", installed, release.TagName), colWarning)
	app.uiManager.showNotice(notice{
		id: ytDlpNoticeID,
		text: fmt.Sprintf("yt-dlp %s is out of date (latest: %s). Sites change often, and an old yt-dlp is the most common reason downloads stop working.",
			installed, release.TagName),
		actionLabel: "Update now",
		action:      app.uiManager.runUpdateInUI,
	})
}

// installedYtDlpVersion returns the version the installed yt-dlp reports.
func (app *DownloaderApp) installedYtDlpVersion() (string, error) {
	output, err := app.depSvc.Version("yt-dlp")
	if err != nil {
		return "", err
	}
	version, _, _ := strings.Cut(output, "\n")
	return strings.TrimSpace(version), nil
}

// ytDlpVersions returns the installed yt-dlp version and the latest release,
// either of which is unknownVersion when it cannot be determined. It runs
// yt-dlp and may ask GitHub, so call it off the UI thread.
func (app *DownloaderApp) ytDlpVersions() (installed, latest string) {
	installed, latest = unknownVersion, unknownVersion
	if version, err := app.installedYtDlpVersion(); err == nil && version != "" {
		installed = version
	}
	if release, err := app.releaseSvc.Latest(context.Background(), ytDlpOwner, ytDlpRepo, releaseCheckInterval); err == nil {
		latest = release.TagName
	}
	return installed, latest
}
