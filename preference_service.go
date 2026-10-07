// preference_service.go — Centralized preference loading and persistence.
//
// Responsibilities:
//   - AppPreferences: plain value struct that mirrors every stored preference key,
//     making the full set of configurable options visible in one place.
//   - PreferenceService: reads from and writes to the Fyne Preferences store,
//     applying fallbacks where appropriate. Has no dependency on any UI widget.
//   - LoadFromFile / MergeConfig / ExportConfig: govid.json, whose AppConfig
//     type and rules live in config_file.go.
//   - Named constants for every preference key and default value.
package main

import (
	"math"
	"os"
	"path/filepath"
	"runtime"

	"fyne.io/fyne/v2"
)

// ── Preference key constants ─────────────────────────────────────────────────
// Single source of truth for the storage keys used throughout the application.
// Using constants instead of inline string literals prevents silent typos.
const (
	prefSavedPath         = "savedPath"
	prefFormat            = "format"
	prefQuality           = "quality"
	prefMaxSpeed          = "maxSpeed"
	prefThemeMode         = "themeMode"
	prefSavePrefs         = "savePrefs"
	prefCookiesPath       = "cookiesPath"
	prefCookieSource      = "cookieSource"
	prefCookieBrowser     = "cookieBrowser"
	prefCookieProfile     = "cookieProfile"
	prefSimultaneous      = "simultaneousDownloads"
	prefPreferredCodec    = "preferredCodec"
	prefLogLimit          = "logLimit"
	prefShowDebug         = "showDebug"
	prefCheckUpdates      = "checkUpdates"
	prefEmbedMetadata     = "embedMetadata"
	prefEmbedThumbnail    = "embedThumbnail"
	prefEmbedChapters     = "embedChapters"
	prefSubtitles         = "subtitles"
	prefSubtitleLangs     = "subtitleLangs"
	prefAutoSubtitles     = "autoSubtitles"
	prefKeepHistory       = "keepHistory"
	prefBatchMode         = "batchMode"
	prefSaveLog           = "saveLog"
	prefNotify            = "notify"
	prefAutoRetry         = "autoRetry"
	prefEnablePostProcess = "enablePostProcess"
	prefSmoothMotion      = "smoothMotion"
	prefSmoothMotionMode  = "smoothMotionMode"
	prefSmoothFPS         = "smoothFPS"
	prefSharpen           = "sharpen"
	prefSharpenAmount     = "sharpenAmount"
	prefNormalize         = "normalize"
	prefVividMode         = "vividMode"
	prefDenoise           = "denoise"
	prefDenoiseMode       = "denoiseMode"
	prefHDRToSDR          = "hdrToSdr"
	prefDeband            = "deband"
	prefAutoCrop          = "autoCrop"
	prefStabilize         = "stabilize"
	prefDeinterlace       = "deinterlace"
	prefNightMode         = "nightMode"
	prefUpscaleVideo      = "upscaleVideo"
	prefUpscaleTarget     = "upscaleTarget"
	prefGPUBackend        = "gpuBackend"
)

// legacyPrefSmoothMotion is the key Smooth Motion was stored under before the
// separate Upscale feature (prefUpscaleVideo) existed. migrateLegacyKeys moves
// it to prefSmoothMotion.
const legacyPrefSmoothMotion = "upscale"

// ── Default values ────────────────────────────────────────────────────────────
// Named so they can be used for both Load fallbacks and UI resets without
// scattering magic literals throughout the codebase.
const (
	defaultThemeMode         = themeSystem
	defaultQuality           = qualityBest
	defaultSavePrefs         = true
	defaultCheckUpdates      = true
	defaultKeepHistory       = true
	defaultEmbedMetadata     = true
	defaultEmbedThumbnail    = true
	defaultSubtitles         = subtitlesOff
	defaultSubtitleLangs     = "en.*"
	defaultSmoothMotionMode  = smoothModeBalanced
	defaultSmoothFPS         = 60.0
	defaultDenoiseMode       = denoiseModeHQDN3D
	defaultLogLimit          = "200"
	defaultUpscaleTarget     = upscaleDouble
	defaultSharpenAmount     = 1.0
	defaultEnablePostProcess = true
	defaultGPUBackend        = gpuBackendLabelAuto
	defaultCookieBrowser     = browserFirefox
	defaultSimultaneous      = "1"
	defaultPreferredCodec    = codecAny
)

// AppPreferences is a plain value struct that mirrors every user preference.
// It holds no Fyne widget references and is safe to construct, copy, and pass
// between functions without touching the UI thread.
type AppPreferences struct {
	SavePrefs         bool
	SavedPath         string
	Format            string
	Quality           string
	MaxSpeed          string
	ThemeMode         string
	CookiesPath       string
	CookieSource      string // one of cookieSourceOptions
	CookieBrowser     string // one of cookieBrowserOptions
	CookieProfile     string // the browser profile to read cookies from; "" for the default
	Simultaneous      string // how many downloads run at once, one of simultaneousOptions
	PreferredCodec    string // one of preferredCodecOptions: the video codec downloads prefer
	LogLimit          string
	ShowDebug         bool   // show yt-dlp [debug] lines in the log view (they always go to the log file)
	CheckUpdates      bool   // check for newer yt-dlp and GoVid releases on startup
	EmbedMetadata     bool   // write title, artist, date, … tags into downloaded files
	EmbedThumbnail    bool   // write the thumbnail into downloaded files as cover art
	EmbedChapters     bool   // write chapter markers into downloaded files
	Subtitles         string // one of subtitleModeOptions
	SubtitleLangs     string // yt-dlp --sub-langs list, e.g. "en.*,de"
	AutoSubtitles     bool   // also take auto-generated captions when a language has no subtitles
	KeepHistory       bool   // record each download in download_history.json
	BatchMode         bool
	SaveLog           bool
	Notify            bool
	AutoRetry         bool
	EnablePostProcess bool
	SmoothMotion      bool
	SmoothMotionMode  string
	SmoothFPS         float64
	Sharpen           bool
	SharpenAmount     float64
	NormalizeAudio    bool
	VividMode         bool
	Denoise           bool
	DenoiseMode       string
	HDRToSDR          bool
	Deband            bool
	AutoCrop          bool
	Stabilize         bool
	Deinterlace       bool
	NightMode         bool
	UpscaleVideo      bool
	UpscaleTarget     string
	GPUBackend        string
}

// PreferenceService reads from and writes to a Fyne Preferences store.
// It has no dependency on any UI widget and owns all preference key names
// and default values.
type PreferenceService struct {
	store fyne.Preferences
}

// NewPreferenceService constructs a PreferenceService backed by the given
// Fyne Preferences store. Pass fyne.CurrentApp().Preferences() at startup.
func NewPreferenceService(store fyne.Preferences) *PreferenceService {
	prefSvc := &PreferenceService{store: store}
	prefSvc.migrateLegacyKeys()
	return prefSvc
}

// migrateLegacyKeys moves values stored under retired keys to their current
// keys, so settings saved by older versions are not lost.
func (prefSvc *PreferenceService) migrateLegacyKeys() {
	// A stored bool reads back the same whatever the fallback; an absent key
	// reads back the fallback.
	stored := prefSvc.store.BoolWithFallback(legacyPrefSmoothMotion, false) ==
		prefSvc.store.BoolWithFallback(legacyPrefSmoothMotion, true)
	if !stored {
		return
	}
	prefSvc.store.SetBool(prefSmoothMotion, prefSvc.store.Bool(legacyPrefSmoothMotion))
	prefSvc.store.RemoveValue(legacyPrefSmoothMotion)
}

// Load reads every stored preference and returns an AppPreferences with
// fallback defaults applied for any key that has not been explicitly set.
func (prefSvc *PreferenceService) Load() AppPreferences {
	p := AppPreferences{
		SavePrefs:         prefSvc.store.BoolWithFallback(prefSavePrefs, defaultSavePrefs),
		SavedPath:         prefSvc.store.String(prefSavedPath),
		Format:            prefSvc.store.String(prefFormat),
		Quality:           prefSvc.store.String(prefQuality),
		MaxSpeed:          prefSvc.store.String(prefMaxSpeed),
		ThemeMode:         prefSvc.store.StringWithFallback(prefThemeMode, defaultThemeMode),
		CookiesPath:       prefSvc.store.String(prefCookiesPath),
		CookieSource:      prefSvc.store.String(prefCookieSource),
		CookieBrowser:     prefSvc.store.StringWithFallback(prefCookieBrowser, defaultCookieBrowser),
		CookieProfile:     prefSvc.store.String(prefCookieProfile),
		Simultaneous:      prefSvc.store.StringWithFallback(prefSimultaneous, defaultSimultaneous),
		PreferredCodec:    prefSvc.store.StringWithFallback(prefPreferredCodec, defaultPreferredCodec),
		LogLimit:          prefSvc.store.StringWithFallback(prefLogLimit, defaultLogLimit),
		ShowDebug:         prefSvc.store.Bool(prefShowDebug),
		CheckUpdates:      prefSvc.store.BoolWithFallback(prefCheckUpdates, defaultCheckUpdates),
		EmbedMetadata:     prefSvc.store.BoolWithFallback(prefEmbedMetadata, defaultEmbedMetadata),
		EmbedThumbnail:    prefSvc.store.BoolWithFallback(prefEmbedThumbnail, defaultEmbedThumbnail),
		EmbedChapters:     prefSvc.store.Bool(prefEmbedChapters),
		Subtitles:         prefSvc.store.StringWithFallback(prefSubtitles, defaultSubtitles),
		SubtitleLangs:     prefSvc.store.StringWithFallback(prefSubtitleLangs, defaultSubtitleLangs),
		AutoSubtitles:     prefSvc.store.Bool(prefAutoSubtitles),
		KeepHistory:       prefSvc.store.BoolWithFallback(prefKeepHistory, defaultKeepHistory),
		BatchMode:         prefSvc.store.Bool(prefBatchMode),
		SaveLog:           prefSvc.store.Bool(prefSaveLog),
		Notify:            prefSvc.store.Bool(prefNotify),
		AutoRetry:         prefSvc.store.Bool(prefAutoRetry),
		EnablePostProcess: prefSvc.store.BoolWithFallback(prefEnablePostProcess, defaultEnablePostProcess),
		SmoothMotion:      prefSvc.store.Bool(prefSmoothMotion),
		SmoothMotionMode:  prefSvc.store.StringWithFallback(prefSmoothMotionMode, defaultSmoothMotionMode),
		SmoothFPS:         prefSvc.store.FloatWithFallback(prefSmoothFPS, defaultSmoothFPS),
		Sharpen:           prefSvc.store.Bool(prefSharpen),
		SharpenAmount:     math.Round(prefSvc.store.FloatWithFallback(prefSharpenAmount, defaultSharpenAmount)*10) / 10,
		NormalizeAudio:    prefSvc.store.Bool(prefNormalize),
		VividMode:         prefSvc.store.Bool(prefVividMode),
		Denoise:           prefSvc.store.Bool(prefDenoise),
		DenoiseMode:       prefSvc.store.StringWithFallback(prefDenoiseMode, defaultDenoiseMode),
		HDRToSDR:          prefSvc.store.Bool(prefHDRToSDR),
		Deband:            prefSvc.store.Bool(prefDeband),
		AutoCrop:          prefSvc.store.Bool(prefAutoCrop),
		Stabilize:         prefSvc.store.Bool(prefStabilize),
		Deinterlace:       prefSvc.store.Bool(prefDeinterlace),
		NightMode:         prefSvc.store.Bool(prefNightMode),
		UpscaleVideo:      prefSvc.store.Bool(prefUpscaleVideo),
		UpscaleTarget:     prefSvc.store.StringWithFallback(prefUpscaleTarget, defaultUpscaleTarget),
		GPUBackend:        prefSvc.store.StringWithFallback(prefGPUBackend, defaultGPUBackend),
	}
	return resolveDefaults(p)
}

// resolveDefaults fills the save path, format, quality, and cookie source
// when they are empty. Unlike the fixed defaults applied in Load, an
// explicitly stored empty value also falls back, since none of them is
// usable empty.
// The save path and format defaults depend on the install location and
// platform, so they are computed rather than constant.
func resolveDefaults(p AppPreferences) AppPreferences {
	if p.SavedPath == "" {
		p.SavedPath = defaultSavePath()
	}
	if p.Format == "" {
		p.Format = defaultFormat()
	}
	if p.Quality == "" {
		p.Quality = defaultQuality
	}
	// Before the Cookies choice existed, a cookies file was used whenever
	// one was set, so settings from then keep using it.
	if p.CookieSource == "" {
		p.CookieSource = cookieSourceNone
		if p.CookiesPath != "" {
			p.CookieSource = cookieSourceFile
		}
	}
	return p
}

// defaultFormat returns MP4 on Windows and macOS, where it plays natively,
// and MKV elsewhere.
func defaultFormat() string {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return formatMP4
	}
	return formatMKV
}

// defaultSavePath returns the directory containing the executable, falling
// back to the working directory, or "" if neither can be determined.
func defaultSavePath() string {
	if exePath, err := os.Executable(); err == nil {
		return filepath.Dir(exePath)
	}
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return ""
}

// Save writes the given AppPreferences to the Fyne store.
// The savePrefs toggle is always written. All other keys are only written when
// savePrefs is true, preserving the historic behaviour that lets users opt out
// of persistence while still remembering their opt-out choice.
func (prefSvc *PreferenceService) Save(p AppPreferences) {
	prefSvc.store.SetBool(prefSavePrefs, p.SavePrefs)
	if !p.SavePrefs {
		return
	}
	prefSvc.write(p)
}

// write stores every preference in p, whatever "Save preferences" says;
// Portable Mode uses it to copy all settings to their new place.
func (prefSvc *PreferenceService) write(p AppPreferences) {
	prefSvc.store.SetBool(prefSavePrefs, p.SavePrefs)
	prefSvc.store.SetString(prefSavedPath, p.SavedPath)
	prefSvc.store.SetString(prefFormat, p.Format)
	prefSvc.store.SetString(prefQuality, p.Quality)
	prefSvc.store.SetString(prefMaxSpeed, p.MaxSpeed)
	prefSvc.store.SetString(prefThemeMode, p.ThemeMode)
	prefSvc.store.SetString(prefCookiesPath, p.CookiesPath)
	prefSvc.store.SetString(prefCookieSource, p.CookieSource)
	prefSvc.store.SetString(prefCookieBrowser, p.CookieBrowser)
	prefSvc.store.SetString(prefCookieProfile, p.CookieProfile)
	prefSvc.store.SetString(prefSimultaneous, p.Simultaneous)
	prefSvc.store.SetString(prefPreferredCodec, p.PreferredCodec)
	prefSvc.store.SetString(prefLogLimit, p.LogLimit)
	prefSvc.store.SetBool(prefShowDebug, p.ShowDebug)
	prefSvc.store.SetBool(prefCheckUpdates, p.CheckUpdates)
	prefSvc.store.SetBool(prefEmbedMetadata, p.EmbedMetadata)
	prefSvc.store.SetBool(prefEmbedThumbnail, p.EmbedThumbnail)
	prefSvc.store.SetBool(prefEmbedChapters, p.EmbedChapters)
	prefSvc.store.SetString(prefSubtitles, p.Subtitles)
	prefSvc.store.SetString(prefSubtitleLangs, p.SubtitleLangs)
	prefSvc.store.SetBool(prefAutoSubtitles, p.AutoSubtitles)
	prefSvc.store.SetBool(prefKeepHistory, p.KeepHistory)
	prefSvc.store.SetBool(prefBatchMode, p.BatchMode)
	prefSvc.store.SetBool(prefSaveLog, p.SaveLog)
	prefSvc.store.SetBool(prefNotify, p.Notify)
	prefSvc.store.SetBool(prefAutoRetry, p.AutoRetry)
	prefSvc.store.SetBool(prefEnablePostProcess, p.EnablePostProcess)
	prefSvc.store.SetBool(prefSmoothMotion, p.SmoothMotion)
	prefSvc.store.SetString(prefSmoothMotionMode, p.SmoothMotionMode)
	prefSvc.store.SetFloat(prefSmoothFPS, p.SmoothFPS)
	prefSvc.store.SetBool(prefSharpen, p.Sharpen)
	prefSvc.store.SetFloat(prefSharpenAmount, math.Round(p.SharpenAmount*10)/10)
	prefSvc.store.SetBool(prefNormalize, p.NormalizeAudio)
	prefSvc.store.SetBool(prefVividMode, p.VividMode)
	prefSvc.store.SetBool(prefDenoise, p.Denoise)
	prefSvc.store.SetString(prefDenoiseMode, p.DenoiseMode)
	prefSvc.store.SetBool(prefHDRToSDR, p.HDRToSDR)
	prefSvc.store.SetBool(prefDeband, p.Deband)
	prefSvc.store.SetBool(prefAutoCrop, p.AutoCrop)
	prefSvc.store.SetBool(prefStabilize, p.Stabilize)
	prefSvc.store.SetBool(prefDeinterlace, p.Deinterlace)
	prefSvc.store.SetBool(prefNightMode, p.NightMode)
	prefSvc.store.SetBool(prefUpscaleVideo, p.UpscaleVideo)
	prefSvc.store.SetString(prefUpscaleTarget, p.UpscaleTarget)
	prefSvc.store.SetString(prefGPUBackend, p.GPUBackend)
}

// Reset removes every preference key managed by this service from the Fyne
// store, so the next Load call returns defaults across the board.
func (prefSvc *PreferenceService) Reset() {
	for _, key := range []string{
		prefSavedPath, prefFormat, prefQuality, prefMaxSpeed, prefThemeMode,
		prefSavePrefs, prefCookiesPath, prefCookieSource, prefCookieBrowser, prefCookieProfile, prefSimultaneous, prefPreferredCodec, prefLogLimit, prefShowDebug, prefCheckUpdates,
		prefEmbedMetadata, prefEmbedThumbnail, prefEmbedChapters,
		prefSubtitles, prefSubtitleLangs, prefAutoSubtitles,
		prefKeepHistory,
		prefBatchMode, prefSaveLog, prefNotify, prefAutoRetry, prefEnablePostProcess,
		prefSmoothMotion, prefSmoothMotionMode, prefSmoothFPS,
		prefSharpen, prefSharpenAmount, prefNormalize, prefVividMode,
		prefDenoise, prefDenoiseMode, prefHDRToSDR, prefDeband,
		prefAutoCrop, prefStabilize, prefDeinterlace, prefNightMode,
		prefUpscaleVideo, prefUpscaleTarget,
		prefGPUBackend,
	} {
		prefSvc.store.RemoveValue(key)
	}
}
