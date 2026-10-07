// options.go — Named values for every enum-like option shown in the UI.
//
// Each selector's labels are defined once here, as constants plus an ordered
// option slice. The widget constructors, the preference defaults, the
// yt-dlp and FFmpeg argument builders, and the help text all reference
// these names, so renaming a label in one place cannot silently send a
// selection down another code path's default branch.
//
// isAudioOnlyExt applies the audio-only formats to file extensions. GPU
// backend labels live with the backend definitions in gpu_capability.go.
package main

import "strings"

// Output formats (main window "Output Format").
const (
	formatMP4  = "MP4"
	formatMKV  = "MKV"
	formatWebM = "WebM"
	formatMP3  = "MP3"
	formatM4A  = "M4A"
)

// formatOptions lists the output formats in display order.
var formatOptions = []string{formatMP4, formatMKV, formatWebM, formatMP3, formatM4A}

// isAudioOnlyExt reports whether a file extension, with or without the
// leading dot and in any case, is one of the audio-only output formats.
// Audio-only files get yt-dlp's --extract-audio and skip video filters.
func isAudioOnlyExt(ext string) bool {
	ext = strings.TrimPrefix(ext, ".")
	return strings.EqualFold(ext, formatMP3) || strings.EqualFold(ext, formatM4A)
}

// Maximum download qualities (main window "Max Quality").
const (
	qualityBest  = "Best Quality"
	quality1080p = "1080p"
	quality720p  = "720p"
	quality480p  = "480p"
	quality360p  = "360p"
)

// qualityOptions lists the quality caps in display order.
var qualityOptions = []string{qualityBest, quality1080p, quality720p, quality480p, quality360p}

// Application themes (Preferences "Application Theme"). System follows the
// operating system's light or dark setting.
const (
	themeSystem = "System"
	themeDark   = "Dark"
	themeLight  = "Light"
)

// themeOptions lists the themes in display order.
var themeOptions = []string{themeSystem, themeDark, themeLight}

// logLimitUnlimited is the Log Buffer Limit option that disables trimming.
const logLimitUnlimited = "Unlimited"

// logLimitOptions lists the Log Buffer Limit choices in display order. Every
// entry other than logLimitUnlimited is a line count.
var logLimitOptions = []string{"100", "200", "500", "1000", "5000", logLimitUnlimited}

// Smooth Motion modes (Post-Processing "Smoothing Mode").
const (
	smoothModePrecise  = "Precise (slow)"
	smoothModeBalanced = "Balanced"
	smoothModeFast     = "Fast"
)

// smoothModeOptions lists the Smooth Motion modes in display order.
var smoothModeOptions = []string{smoothModePrecise, smoothModeBalanced, smoothModeFast}

// Denoise methods (Post-Processing "Denoise Mode").
const (
	denoiseModeNLMeans = "NLMeans (HQ, slow)"
	denoiseModeHQDN3D  = "hqdn3d (Balanced)"
)

// denoiseModeOptions lists the denoise methods in display order.
var denoiseModeOptions = []string{denoiseModeNLMeans, denoiseModeHQDN3D}

// Upscale targets (Post-Processing "Target Resolution").
const (
	upscaleDouble = "2× (Double)"
	upscale1080p  = "1080p"
	upscale1440p  = "1440p"
	upscale4K     = "4K (2160p)"
)

// upscaleTargetOptions lists the upscale targets in display order.
var upscaleTargetOptions = []string{upscaleDouble, upscale1080p, upscale1440p, upscale4K}

// Subtitle modes (Preferences "Subtitles").
const (
	subtitlesOff   = "Off"
	subtitlesEmbed = "Embed"
	subtitlesSRT   = "Save as .srt"
	subtitlesBoth  = "Both"
)

// subtitleModeOptions lists the subtitle modes in display order.
var subtitleModeOptions = []string{subtitlesOff, subtitlesEmbed, subtitlesSRT, subtitlesBoth}

// Where yt-dlp gets cookies from (Preferences "Cookies").
const (
	cookieSourceNone    = "None"
	cookieSourceFile    = "From file"
	cookieSourceBrowser = "From browser"
)

// cookieSourceOptions lists the cookie sources in display order.
var cookieSourceOptions = []string{cookieSourceNone, cookieSourceFile, cookieSourceBrowser}

// Browsers yt-dlp can read cookies from (--cookies-from-browser). yt-dlp's
// name for each is the label in lower case.
const (
	browserFirefox  = "Firefox"
	browserChrome   = "Chrome"
	browserEdge     = "Edge"
	browserBrave    = "Brave"
	browserChromium = "Chromium"
	browserOpera    = "Opera"
	browserVivaldi  = "Vivaldi"
	browserWhale    = "Whale"
	browserSafari   = "Safari"
)

// cookieBrowserOptions lists the browsers in display order. Firefox comes
// first because on Windows it is the one yt-dlp can always read: Chrome,
// Edge, and the other Chromium browsers lock their cookies while they run
// and encrypt them with app-bound encryption.
var cookieBrowserOptions = []string{
	browserFirefox, browserChrome, browserEdge, browserBrave, browserChromium,
	browserOpera, browserVivaldi, browserWhale, browserSafari,
}

// simultaneousOptions lists how many downloads may run at once
// (Preferences "Simultaneous Downloads").
var simultaneousOptions = []string{"1", "2", "3"}
