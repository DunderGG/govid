// preference_service.go — Centralized preference loading and persistence.
//
// Responsibilities:
//   - AppPreferences: plain value struct that mirrors every stored preference key,
//     making the full set of configurable options visible in one place.
//   - PreferenceService: reads from and writes to the Fyne Preferences store,
//     applying fallbacks where appropriate. Has no dependency on any UI widget.
//   - Named constants for every preference key and default value.
package main

import "fyne.io/fyne/v2"

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
	prefLogLimit          = "logLimit"
	prefBatchMode         = "batchMode"
	prefSaveLog           = "saveLog"
	prefNotify            = "notify"
	prefAutoRetry         = "autoRetry"
	prefEnablePostProcess = "enablePostProcess"
	prefSmoothMotion      = "upscale"
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
)

// ── Default values ────────────────────────────────────────────────────────────
// Named so they can be used for both Load fallbacks and UI resets without
// scattering magic literals throughout the codebase.
const (
	defaultThemeMode         = "Dark"
	defaultSavePrefs         = true
	defaultSmoothMotionMode  = "Balanced"
	defaultSmoothFPS         = 60.0
	defaultDenoiseMode       = "hqdn3d (Balanced)"
	defaultLogLimit          = "200"
	defaultUpscaleTarget     = "2× (Double)"
	defaultSharpenAmount     = 1.0
	defaultEnablePostProcess = true
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
	LogLimit          string
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
	return &PreferenceService{store: store}
}

// Load reads every stored preference and returns an AppPreferences with
// fallback defaults applied for any key that has not been explicitly set.
func (prefSvc *PreferenceService) Load() AppPreferences {
	return AppPreferences{
		SavePrefs:         prefSvc.store.BoolWithFallback(prefSavePrefs, defaultSavePrefs),
		SavedPath:         prefSvc.store.String(prefSavedPath),
		Format:            prefSvc.store.String(prefFormat),
		Quality:           prefSvc.store.String(prefQuality),
		MaxSpeed:          prefSvc.store.String(prefMaxSpeed),
		ThemeMode:         prefSvc.store.StringWithFallback(prefThemeMode, defaultThemeMode),
		CookiesPath:       prefSvc.store.String(prefCookiesPath),
		LogLimit:          prefSvc.store.StringWithFallback(prefLogLimit, defaultLogLimit),
		BatchMode:         prefSvc.store.Bool(prefBatchMode),
		SaveLog:           prefSvc.store.Bool(prefSaveLog),
		Notify:            prefSvc.store.Bool(prefNotify),
		AutoRetry:         prefSvc.store.Bool(prefAutoRetry),
		EnablePostProcess: prefSvc.store.BoolWithFallback(prefEnablePostProcess, defaultEnablePostProcess),
		SmoothMotion:      prefSvc.store.Bool(prefSmoothMotion),
		SmoothMotionMode:  prefSvc.store.StringWithFallback(prefSmoothMotionMode, defaultSmoothMotionMode),
		SmoothFPS:         prefSvc.store.FloatWithFallback(prefSmoothFPS, defaultSmoothFPS),
		Sharpen:           prefSvc.store.Bool(prefSharpen),
		SharpenAmount:     prefSvc.store.FloatWithFallback(prefSharpenAmount, defaultSharpenAmount),
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
	}
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
	prefSvc.store.SetString(prefSavedPath, p.SavedPath)
	prefSvc.store.SetString(prefFormat, p.Format)
	prefSvc.store.SetString(prefQuality, p.Quality)
	prefSvc.store.SetString(prefMaxSpeed, p.MaxSpeed)
	prefSvc.store.SetString(prefThemeMode, p.ThemeMode)
	prefSvc.store.SetString(prefCookiesPath, p.CookiesPath)
	prefSvc.store.SetString(prefLogLimit, p.LogLimit)
	prefSvc.store.SetBool(prefBatchMode, p.BatchMode)
	prefSvc.store.SetBool(prefSaveLog, p.SaveLog)
	prefSvc.store.SetBool(prefNotify, p.Notify)
	prefSvc.store.SetBool(prefAutoRetry, p.AutoRetry)
	prefSvc.store.SetBool(prefEnablePostProcess, p.EnablePostProcess)
	prefSvc.store.SetBool(prefSmoothMotion, p.SmoothMotion)
	prefSvc.store.SetString(prefSmoothMotionMode, p.SmoothMotionMode)
	prefSvc.store.SetFloat(prefSmoothFPS, p.SmoothFPS)
	prefSvc.store.SetBool(prefSharpen, p.Sharpen)
	prefSvc.store.SetFloat(prefSharpenAmount, p.SharpenAmount)
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
}

// Reset removes every preference key managed by this service from the Fyne
// store, so the next Load call returns defaults across the board.
func (prefSvc *PreferenceService) Reset() {
	for _, key := range []string{
		prefSavedPath, prefFormat, prefQuality, prefMaxSpeed, prefThemeMode,
		prefSavePrefs, prefCookiesPath, prefLogLimit,
		prefBatchMode, prefSaveLog, prefNotify, prefAutoRetry, prefEnablePostProcess,
		prefSmoothMotion, prefSmoothMotionMode, prefSmoothFPS,
		prefSharpen, prefSharpenAmount, prefNormalize, prefVividMode,
		prefDenoise, prefDenoiseMode, prefHDRToSDR, prefDeband,
		prefAutoCrop, prefStabilize, prefDeinterlace, prefNightMode,
		prefUpscaleVideo, prefUpscaleTarget,
	} {
		prefSvc.store.RemoveValue(key)
	}
}
