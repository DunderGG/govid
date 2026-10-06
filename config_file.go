// config_file.go — govid.json: every preference as an optional setting.
//
// Responsibilities:
//   - AppConfig: one pointer field per AppPreferences field, with the same
//     name, so a key left out of the file changes nothing. A test checks
//     that every preference has a key, so a new preference cannot be
//     forgotten here.
//   - configRules: what each value must be: one of the options in
//     options.go, a number in the range its control allows, or an existing
//     folder or file.
//   - PreferenceService.LoadFromFile, MergeConfig, ExportConfig, and
//     WriteConfigFile: reading the file, applying it to preferences,
//     turning preferences back into a complete file, and writing it.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
)

// configFileName is the optional JSON override file read by "Load from Config".
const configFileName = "govid.json"

// AppConfig is govid.json. Every field mirrors the AppPreferences field of the
// same name, as a pointer: nil means the key was left out, and the setting
// keeps its value. Exported files hold every key; presets hold only some.
type AppConfig struct {
	SavePrefs         *bool    `json:"savePrefs,omitempty"`
	SavedPath         *string  `json:"path,omitempty"`
	Format            *string  `json:"format,omitempty"`
	Quality           *string  `json:"quality,omitempty"`
	MaxSpeed          *string  `json:"maxSpeed,omitempty"`
	ThemeMode         *string  `json:"themeMode,omitempty"`
	CookiesPath       *string  `json:"cookiesPath,omitempty"`
	LogLimit          *string  `json:"logLimit,omitempty"`
	ShowDebug         *bool    `json:"showDebug,omitempty"`
	CheckUpdates      *bool    `json:"checkUpdates,omitempty"`
	EmbedMetadata     *bool    `json:"embedMetadata,omitempty"`
	EmbedThumbnail    *bool    `json:"embedThumbnail,omitempty"`
	EmbedChapters     *bool    `json:"embedChapters,omitempty"`
	Subtitles         *string  `json:"subtitles,omitempty"`
	SubtitleLangs     *string  `json:"subtitleLangs,omitempty"`
	AutoSubtitles     *bool    `json:"autoSubtitles,omitempty"`
	KeepHistory       *bool    `json:"keepHistory,omitempty"`
	BatchMode         *bool    `json:"batchMode,omitempty"`
	SaveLog           *bool    `json:"saveLog,omitempty"`
	Notify            *bool    `json:"notify,omitempty"`
	AutoRetry         *bool    `json:"autoRetry,omitempty"`
	EnablePostProcess *bool    `json:"postProcess,omitempty"`
	SmoothMotion      *bool    `json:"smoothMotion,omitempty"`
	SmoothMotionMode  *string  `json:"smoothMotionMode,omitempty"`
	SmoothFPS         *float64 `json:"smoothFPS,omitempty"`
	Sharpen           *bool    `json:"sharpen,omitempty"`
	SharpenAmount     *float64 `json:"sharpenAmount,omitempty"`
	NormalizeAudio    *bool    `json:"normalizeAudio,omitempty"`
	VividMode         *bool    `json:"vividMode,omitempty"`
	Denoise           *bool    `json:"denoise,omitempty"`
	DenoiseMode       *string  `json:"denoiseMode,omitempty"`
	HDRToSDR          *bool    `json:"hdrToSdr,omitempty"`
	Deband            *bool    `json:"deband,omitempty"`
	AutoCrop          *bool    `json:"autoCrop,omitempty"`
	Stabilize         *bool    `json:"stabilize,omitempty"`
	Deinterlace       *bool    `json:"deinterlace,omitempty"`
	NightMode         *bool    `json:"nightMode,omitempty"`
	UpscaleVideo      *bool    `json:"upscaleVideo,omitempty"`
	UpscaleTarget     *string  `json:"upscaleTarget,omitempty"`
	GPUBackend        *string  `json:"gpuBackend,omitempty"`
}

// configRule says what a govid.json value must be. A field without a rule
// takes any value of its type.
type configRule struct {
	options    func() []string    // a string must be one of these
	min, max   float64            // a number must be within [min, max] when max > min
	check      func(string) error // any other check of a string
	emptyKeeps bool               // "" leaves the setting unchanged (always so with options)
}

// configRules holds the rules of the AppConfig fields that need one, by
// field name. The option lists are those the widgets offer, and the number
// ranges those of the sliders (see NewPostProcessControls).
var configRules = map[string]configRule{
	"SavedPath":        {check: checkFolder, emptyKeeps: true},
	"CookiesPath":      {check: checkFileOrEmpty},
	"Format":           {options: func() []string { return formatOptions }},
	"Quality":          {options: func() []string { return qualityOptions }},
	"ThemeMode":        {options: func() []string { return themeOptions }},
	"LogLimit":         {options: func() []string { return logLimitOptions }},
	"Subtitles":        {options: func() []string { return subtitleModeOptions }},
	"SmoothMotionMode": {options: func() []string { return smoothModeOptions }},
	"DenoiseMode":      {options: func() []string { return denoiseModeOptions }},
	"UpscaleTarget":    {options: func() []string { return upscaleTargetOptions }},
	"GPUBackend":       {options: GPUBackendOptions},
	"SmoothFPS":        {min: 24, max: 120},
	"SharpenAmount":    {min: 0, max: 2},
}

// checkFolder reports an error unless path is an existing folder.
func checkFolder(path string) error {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("%q is not an existing folder", path)
	}
	return nil
}

// checkFileOrEmpty reports an error unless path is "" (none) or an existing file.
func checkFileOrEmpty(path string) error {
	if path == "" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return fmt.Errorf("%q is not an existing file", path)
	}
	return nil
}

// validate checks value against the rule. keep is true when the value
// leaves the setting unchanged ("" for a choice); err says what is wrong
// with an invalid value.
func (rule configRule) validate(value reflect.Value) (keep bool, err error) {
	switch value.Kind() {
	case reflect.String:
		text := value.String()
		if text == "" && (rule.options != nil || rule.emptyKeeps) {
			return true, nil
		}
		if rule.options != nil {
			if options := rule.options(); !slices.Contains(options, text) {
				return false, fmt.Errorf("%q is not one of %s", text, strings.Join(options, ", "))
			}
		}
		if rule.check != nil {
			return false, rule.check(text)
		}
	case reflect.Float64:
		number := value.Float()
		if rule.max > rule.min && (number < rule.min || number > rule.max) {
			return false, fmt.Errorf("%g is outside %g–%g", number, rule.min, rule.max)
		}
	}
	return false, nil
}

// configKey returns the JSON key of an AppConfig field.
func configKey(field reflect.StructField) string {
	key, _, _ := strings.Cut(field.Tag.Get("json"), ",")
	return key
}

// parseAppConfig unmarshals raw JSON bytes into an AppConfig. Keys it does
// not know are ignored, so a file from a newer GoVid still loads.
func parseAppConfig(data []byte) (*AppConfig, error) {
	var config AppConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

// LoadFromFile reads and parses a govid.json config override file at the given
// path. Returns (*AppConfig, nil) on success or (nil, err) if the file cannot
// be read or contains invalid JSON.
func (svc *PreferenceService) LoadFromFile(path string) (*AppConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseAppConfig(data)
}

// MergeConfig applies every key cfg sets onto base and returns the result,
// with a message for each value that was skipped because configRules
// rejects it. The others still apply, so all problems are reported at once.
func (svc *PreferenceService) MergeConfig(cfg *AppConfig, base AppPreferences) (AppPreferences, []string) {
	var errs []string
	config := reflect.ValueOf(cfg).Elem()
	merged := reflect.ValueOf(&base).Elem()
	for i := range config.NumField() {
		field := config.Type().Field(i)
		value := config.Field(i)
		if value.IsNil() {
			continue
		}
		keep, err := configRules[field.Name].validate(value.Elem())
		if err != nil {
			errs = append(errs, fmt.Sprintf("invalid %s: %v", configKey(field), err))
			continue
		}
		if !keep {
			merged.FieldByName(field.Name).Set(value.Elem())
		}
	}
	return base, errs
}

// ExportConfig returns p as a complete AppConfig, with every key set, for
// Tools → Export settings.
func (svc *PreferenceService) ExportConfig(p AppPreferences) AppConfig {
	var cfg AppConfig
	config := reflect.ValueOf(&cfg).Elem()
	prefs := reflect.ValueOf(p)
	for i := range config.NumField() {
		value := prefs.FieldByName(config.Type().Field(i).Name)
		pointer := reflect.New(value.Type())
		pointer.Elem().Set(value)
		config.Field(i).Set(pointer)
	}
	return cfg
}

// WriteConfigFile writes cfg to path as indented JSON, replacing the file
// in one step so a failed write cannot leave half a file.
func (svc *PreferenceService) WriteConfigFile(path string, cfg AppConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(data, '\n'))
}
