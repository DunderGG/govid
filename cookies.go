// cookies.go — Passing the user's login (cookies) to yt-dlp, and explaining
// failures that cookies cause or would fix.
//
// Responsibilities:
//   - cookieArgs: --cookies-from-browser BROWSER[:PROFILE] or --cookies FILE,
//     for both the probe and the download, from the Cookies preference
//     (None / From file / From browser).
//   - cookieLabel: how the session log names the cookie source ("Firefox",
//     "file set"), never the file's path or anything in it.
//   - classifyAccessError / accessHint: yt-dlp errors that cookies cause (a
//     browser holding its cookie database locked, Chrome's app-bound
//     encryption, a missing profile) or would fix (YouTube's bot check,
//     age-restricted, members-only, and private videos), and what to do
//     about each, naming the setting to change.
package main

import (
	"fmt"
	"strings"
)

// cookieArgs returns the yt-dlp options that pass req's cookies: from a
// browser, or from a cookies.txt file that exists. Both the probe and the
// download pass them, since both send requests to the site.
func cookieArgs(req DownloadRequest) []string {
	if req.CookiesFromBrowser != "" {
		return []string{"--cookies-from-browser", req.CookiesFromBrowser}
	}
	if req.CookiesPath != "" && fileExists(req.CookiesPath) {
		return []string{"--cookies", req.CookiesPath}
	}
	return nil
}

// cookiesFromBrowser returns the --cookies-from-browser value for a browser
// label (one of cookieBrowserOptions) and an optional profile name, e.g.
// "firefox" or "firefox:work".
func cookiesFromBrowser(browser, profile string) string {
	value := strings.ToLower(browser)
	if profile = strings.TrimSpace(profile); profile != "" {
		value += ":" + profile
	}
	return value
}

// browserLabel returns the label of the browser a --cookies-from-browser
// value names, e.g. "Firefox" for "firefox:work".
func browserLabel(value string) string {
	name, _, _ := strings.Cut(value, ":")
	for _, label := range cookieBrowserOptions {
		if strings.EqualFold(label, name) {
			return label
		}
	}
	return name
}

// cookieLabel describes where the cookies come from, for the session log
// and diagnostics: "none", "file set", "file set, but missing", or the
// browser ("Firefox", "Firefox, profile set"). It never includes the
// file's path, which can name the user, or the profile's name.
func cookieLabel(source, browser, profile, path string) string {
	switch source {
	case cookieSourceFile:
		switch {
		case path == "":
			return "file chosen, but none set"
		case !fileExists(path):
			return "file set, but missing"
		default:
			return "file set"
		}
	case cookieSourceBrowser:
		if strings.TrimSpace(profile) != "" {
			return browser + ", profile set"
		}
		return browser
	default:
		return "none"
	}
}

// accessProblem is a yt-dlp error that cookies caused or would fix.
type accessProblem int

const (
	accessOK accessProblem = iota
	accessBotCheck
	accessAgeRestricted
	accessMembersOnly
	accessPrivate
	cookiesLocked    // the browser is running and holds its cookie database locked
	cookiesAppBound  // the browser encrypts its cookies with app-bound encryption
	cookiesNoProfile // no cookie database for the browser (or the profile named)
)

// classifyAccessError returns the problem a yt-dlp ERROR line reports. The
// cookie errors are the lines the bundled yt-dlp (2026.03.17) printed on
// Windows on 2026-10-07:
//
//	ERROR: Could not copy Chrome cookie database. See  https://github.com/yt-dlp/yt-dlp/issues/7271  for more info
//	ERROR: Failed to decrypt with DPAPI. See  https://github.com/yt-dlp/yt-dlp/issues/10927  for more info
//	ERROR: could not find firefox cookies database in '…'
//
// The first with Chrome's database held open with no sharing (as a running
// Chrome holds it), the second with Chrome and Edge closed (app-bound
// encryption), the third for a Firefox profile that does not exist.
func classifyAccessError(line string) accessProblem {
	if !strings.Contains(line, "ERROR:") {
		return accessOK
	}
	switch {
	case strings.Contains(line, "Could not copy") && strings.Contains(line, "cookie database"):
		return cookiesLocked
	case strings.Contains(line, "Failed to decrypt with DPAPI"), strings.Contains(line, "app-bound"):
		return cookiesAppBound
	case strings.Contains(line, "could not find") && strings.Contains(line, "cookies database"):
		return cookiesNoProfile
	case strings.Contains(line, "not a bot"):
		return accessBotCheck
	case strings.Contains(line, "confirm your age"), strings.Contains(line, "age-restricted"), strings.Contains(line, "inappropriate for some users"):
		return accessAgeRestricted
	case strings.Contains(line, "members-only"), strings.Contains(line, "channel's members"), strings.Contains(line, "Join this channel"):
		return accessMembersOnly
	case strings.Contains(line, "Private video"):
		return accessPrivate
	default:
		return accessOK
	}
}

// cookiesSetting names the Cookies setting, for hints.
const cookiesSetting = "Tools → Preferences → Cookies"

// accessHint explains problem to the user and says what to do, given the
// cookies req passed. It returns "" for accessOK.
func accessHint(problem accessProblem, req DownloadRequest) string {
	browser := browserLabel(req.CookiesFromBrowser)
	switch problem {
	case cookiesLocked:
		return fmt.Sprintf("[SYSTEM] Hint: %s is open and keeps its cookies locked, so yt-dlp could not read them. Close %s completely (also from the system tray) and try again, or choose Firefox, or export a cookies.txt file and choose From file, in %s.", browser, browser, cookiesSetting)
	case cookiesAppBound:
		return fmt.Sprintf("[SYSTEM] Hint: %s encrypts its cookies in a way yt-dlp cannot read on Windows (app-bound encryption). Sign in to the site in Firefox and choose Firefox, or export a cookies.txt file with a browser extension and choose From file, in %s.", browser, cookiesSetting)
	case cookiesNoProfile:
		return fmt.Sprintf("[SYSTEM] Hint: yt-dlp found no %s cookies. Check that %s is installed and has been used to sign in, and that the profile name in %s is right (leave it empty for the default profile).", browser, browser, cookiesSetting)
	case accessBotCheck:
		return "[SYSTEM] Hint: YouTube asked to confirm you are not a bot; being signed in usually gets past this. " + signInAdvice(req, "") +
			" Updating yt-dlp (Tools → Update yt-dlp) can help too."
	case accessAgeRestricted:
		return "[SYSTEM] Hint: this video is age-restricted, so the site shows it only to a signed-in adult account. " + signInAdvice(req, "an account that can watch it")
	case accessMembersOnly:
		return "[SYSTEM] Hint: this video is for the channel's members. " + signInAdvice(req, "a member's account")
	case accessPrivate:
		return "[SYSTEM] Hint: this video is private; if its owner shared it with you, signing in lets you download it. " + signInAdvice(req, "the account it was shared with")
	default:
		return ""
	}
}

// signInAdvice says how to pass a login, given the cookies req already
// passed; account names the account needed ("" for any).
func signInAdvice(req DownloadRequest, account string) string {
	signedIn := "signed in"
	if account != "" {
		signedIn += " with " + account
	}
	switch {
	case req.CookiesFromBrowser != "":
		return fmt.Sprintf("GoVid passed %s's cookies; check that you are %s there, and try again.", browserLabel(req.CookiesFromBrowser), signedIn)
	case req.CookiesPath != "":
		return fmt.Sprintf("GoVid passed your cookies file, which may be out of date. Export it again while %s, or choose From browser, in %s.", signedIn, cookiesSetting)
	default:
		return fmt.Sprintf("Choose From browser in %s and pick a browser where you are %s (Firefox works best on Windows).", cookiesSetting, signedIn)
	}
}
