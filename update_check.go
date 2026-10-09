// update_check.go — Checks for newer yt-dlp and GoVid releases.
//
// Responsibilities:
//   - startUpdateChecks: the background checks run at startup when "Check
//     for updates on startup" is enabled. An outdated yt-dlp is logged and
//     shown as a notice whose "Update now" button runs the yt-dlp updater; a
//     newer GoVid release is shown as a notice that opens its release notes.
//   - ytDlpVersions: the installed and latest yt-dlp versions, for the
//     Update yt-dlp dialog and the About window.
//   - checkGoVidRelease: the on-demand Tools → "Check for GoVid updates".
//
// GoVid's own version is the main.version string injected at build time
// from the git tag (see build.bat / build.sh); a "dev" build is never
// compared with releases.
package main

import (
	"context"
	"fmt"
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

// The GitHub repository GoVid is released from.
const (
	govidOwner = "DunderGG"
	govidRepo  = "govid"
)

// govidNoticeID identifies the "a newer GoVid is available" notice.
const govidNoticeID = "govid-update"

// devVersion is main.version in a build made without a release tag.
const devVersion = "dev"

// startUpdateChecks runs the startup update checks in the background, unless
// enabled is false.
func (app *DownloaderApp) startUpdateChecks(enabled bool) {
	if !enabled {
		return
	}
	go func() {
		ctx := context.Background()
		app.checkYtDlpUpdate(ctx)
		app.checkGoVidUpdate(ctx)
	}()
}

// isNewerRelease reports whether latest is a newer GoVid release than the
// running build, current. A development build is never out of date.
func isNewerRelease(current, latest string) bool {
	return current != devVersion && isOlderVersion(current, latest)
}

// checkGoVidUpdate compares this build with the latest GoVid release (asking
// GitHub at most once a day). When a newer release exists, it logs one line
// and shows a notice that opens the release notes. Development builds are
// not checked.
func (app *DownloaderApp) checkGoVidUpdate(ctx context.Context) {
	if version == devVersion {
		app.logSvc.WriteToFile("[SYSTEM] GoVid update check skipped: this is a development build.")
		return
	}
	release, err := app.releaseSvc.Latest(ctx, govidOwner, govidRepo, releaseCheckInterval)
	if err != nil {
		app.logSvc.WriteToFile(fmt.Sprintf("[SYSTEM] GoVid update check skipped: %v", err))
		return
	}
	if !isNewerRelease(version, release.TagName) {
		app.logSvc.WriteToFile(fmt.Sprintf("[SYSTEM] GoVid %s is up to date.", version))
		return
	}

	app.appendOutput(fmt.Sprintf("[SYSTEM] GoVid %s is available (this is %s).", release.TagName, version), colInfo)
	app.uiManager.showNotice(notice{
		id:          govidNoticeID,
		text:        fmt.Sprintf("GoVid %s is available (you have %s).", release.TagName, version),
		actionLabel: "What's new",
		action:      func() { app.uiManager.showGoVidRelease(release) },
	})
}

// checkGoVidRelease asks GitHub for the latest GoVid release now, ignoring
// the daily cache, for Tools → "Check for GoVid updates". newer reports
// whether it is newer than this build. Call it off the UI thread.
func (app *DownloaderApp) checkGoVidRelease() (release Release, newer bool, err error) {
	release, err = app.releaseSvc.Latest(context.Background(), govidOwner, govidRepo, 0)
	if err != nil {
		return Release{}, false, err
	}
	return release, isNewerRelease(version, release.TagName), nil
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
	return app.depSvc.Version("yt-dlp")
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
