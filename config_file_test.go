package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"
)

// TestEveryPreferenceHasAConfigKey fails when a preference is added to
// AppPreferences without a matching AppConfig field: the same name, a
// pointer to the same type, and a JSON key of its own.
func TestEveryPreferenceHasAConfigKey(t *testing.T) {
	prefsType := reflect.TypeFor[AppPreferences]()
	configType := reflect.TypeFor[AppConfig]()
	keys := map[string]string{}

	for i := range prefsType.NumField() {
		pref := prefsType.Field(i)
		field, ok := configType.FieldByName(pref.Name)
		if !ok {
			t.Errorf("AppPreferences.%s has no AppConfig field; add one so govid.json can set it", pref.Name)
			continue
		}
		if field.Type != reflect.PointerTo(pref.Type) {
			t.Errorf("AppConfig.%s is %v, want *%v", pref.Name, field.Type, pref.Type)
		}
		key := configKey(field)
		if key == "" || key == "-" {
			t.Errorf("AppConfig.%s has no JSON key", pref.Name)
		}
		if other, taken := keys[key]; taken {
			t.Errorf("AppConfig.%s and .%s share the JSON key %q", other, pref.Name, key)
		}
		keys[key] = pref.Name
	}
	if configType.NumField() != prefsType.NumField() {
		t.Errorf("AppConfig has %d fields and AppPreferences %d; every config key must be a preference", configType.NumField(), prefsType.NumField())
	}
	for name := range configRules {
		if _, ok := configType.FieldByName(name); !ok {
			t.Errorf("configRules has a rule for %s, which AppConfig does not have", name)
		}
	}
}

// changedPreferences returns preferences with every field set to a valid
// value that differs from the defaults. Format is WebM because the default
// is MP4 on Windows and macOS but MKV elsewhere (defaultFormat).
func changedPreferences(t *testing.T) AppPreferences {
	t.Helper()
	cookies := filepath.Join(t.TempDir(), "cookies.txt")
	touch(t, cookies)
	return AppPreferences{
		SavePrefs: false, SavedPath: t.TempDir(), Format: formatWebM, Quality: quality480p,
		MaxSpeed: "2M", ThemeMode: themeLight, CookiesPath: cookies, LogLimit: "1000",
		CookieSource: cookieSourceBrowser, CookieBrowser: browserChrome, CookieProfile: "work", Simultaneous: "3",
		ShowDebug: true, CheckUpdates: false, EmbedMetadata: false, EmbedThumbnail: false, EmbedChapters: true,
		Subtitles: subtitlesBoth, SubtitleLangs: "de,fr", AutoSubtitles: true, KeepHistory: false,
		BatchMode: true, SaveLog: true, Notify: true, AutoRetry: true, EnablePostProcess: false,
		SmoothMotion: true, SmoothMotionMode: smoothModeFast, SmoothFPS: 90,
		Sharpen: true, SharpenAmount: 1.7, NormalizeAudio: true, VividMode: true,
		Denoise: true, DenoiseMode: denoiseModeNLMeans, HDRToSDR: true, Deband: true,
		AutoCrop: true, Stabilize: true, Deinterlace: true, NightMode: true,
		UpscaleVideo: true, UpscaleTarget: upscale1440p, GPUBackend: GPUBackendOptions()[1],
	}
}

func TestChangedPreferencesChangesEverySetting(t *testing.T) {
	_ = test.NewApp()
	defaults := NewPreferenceService(test.NewTempApp(t).Preferences()).Load()
	changed := reflect.ValueOf(changedPreferences(t))
	original := reflect.ValueOf(defaults)
	for i := range changed.NumField() {
		if changed.Field(i).Equal(original.Field(i)) {
			t.Errorf("changedPreferences leaves %s at its default, so the round trip would not test it", changed.Type().Field(i).Name)
		}
	}
}

func TestExportedConfigRoundTrip(t *testing.T) {
	_ = test.NewApp()
	svc := NewPreferenceService(test.NewTempApp(t).Preferences())
	want := changedPreferences(t)
	path := filepath.Join(t.TempDir(), configFileName)

	if err := svc.WriteConfigFile(path, svc.ExportConfig(want)); err != nil {
		t.Fatalf("WriteConfigFile: %v", err)
	}
	cfg, err := svc.LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	got, errs := svc.MergeConfig(cfg, svc.Load())

	if len(errs) != 0 {
		t.Errorf("MergeConfig errors = %q, want none", errs)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip changed the preferences:\n got  %+v\n want %+v", got, want)
	}
}

func TestExportedConfigHasEveryKey(t *testing.T) {
	svc := &PreferenceService{}
	data, err := json.Marshal(svc.ExportConfig(AppPreferences{}))
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]any
	if err := json.Unmarshal(data, &keys); err != nil {
		t.Fatal(err)
	}
	if want := reflect.TypeFor[AppConfig]().NumField(); len(keys) != want {
		t.Errorf("exported %d keys, want %d (false and empty values too)", len(keys), want)
	}
}

func TestMergeConfigReportsEveryInvalidValue(t *testing.T) {
	cfg, err := parseAppConfig([]byte(`{
		"format": "AVI", "quality": "4K", "themeMode": "Blue", "logLimit": "7",
		"smoothFPS": 500, "sharpenAmount": -1, "gpuBackend": "Quantum",
		"path": "/no/such/folder", "cookiesPath": "/no/such/cookies.txt",
		"denoiseMode": "Magic", "notify": true, "maxSpeed": ""
	}`))
	if err != nil {
		t.Fatal(err)
	}
	base := AppPreferences{Format: formatMP4, MaxSpeed: "5M", SmoothFPS: 60}

	merged, errs := (&PreferenceService{}).MergeConfig(cfg, base)

	wantKeys := []string{"path", "format", "quality", "themeMode", "cookiesPath", "logLimit", "smoothFPS", "sharpenAmount", "denoiseMode", "gpuBackend"}
	var gotKeys []string
	for _, message := range errs {
		key, _, _ := strings.Cut(strings.TrimPrefix(message, "invalid "), ":")
		gotKeys = append(gotKeys, key)
	}
	if !slices.Equal(gotKeys, wantKeys) {
		t.Errorf("errors for %q\nwant      %q\nmessages: %q", gotKeys, wantKeys, errs)
	}
	if merged.Format != formatMP4 || merged.SmoothFPS != 60 {
		t.Errorf("invalid values were applied: format %q, smoothFPS %v", merged.Format, merged.SmoothFPS)
	}
	if !merged.Notify || merged.MaxSpeed != "" {
		t.Errorf("valid values were not applied: notify %v, maxSpeed %q (\"\" means unlimited)", merged.Notify, merged.MaxSpeed)
	}
}

func TestMergeConfigEmptyChoiceKeepsTheSetting(t *testing.T) {
	cfg, err := parseAppConfig([]byte(`{"format": "", "quality": "", "path": ""}`))
	if err != nil {
		t.Fatal(err)
	}
	base := AppPreferences{Format: formatWebM, Quality: quality720p, SavedPath: "/videos"}

	merged, errs := (&PreferenceService{}).MergeConfig(cfg, base)

	if len(errs) != 0 || merged != base {
		t.Errorf("merged = %+v, errs %q; want an unchanged base", merged, errs)
	}
}

func TestRepositoryConfigFileIsValid(t *testing.T) {
	cfg, err := (&PreferenceService{}).LoadFromFile(configFileName)
	if err != nil {
		t.Fatalf("the repository's %s does not load: %v", configFileName, err)
	}
	data, _ := os.ReadFile(configFileName)
	var keys map[string]any
	if err := json.Unmarshal(data, &keys); err != nil {
		t.Fatal(err)
	}
	if want := reflect.TypeFor[AppConfig]().NumField(); len(keys) != want {
		t.Errorf("%s lists %d keys, want all %d as an example", configFileName, len(keys), want)
	}
	// Its path is the author's own folder, so only the other values are checked.
	cfg.SavedPath = nil
	if _, errs := (&PreferenceService{}).MergeConfig(cfg, AppPreferences{}); len(errs) != 0 {
		t.Errorf("%s has invalid values: %q", configFileName, errs)
	}
}

func TestImportConfigAppliesEverySetting(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	want := changedPreferences(t)
	svc := h.app.prefSvc
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := svc.WriteConfigFile(path, svc.ExportConfig(want)); err != nil {
		t.Fatal(err)
	}

	h.app.uiManager.importConfig(path, h.window)

	got := snapshotPreferences(h.app.ui, h.app.ui.download.path.Text)
	got.SharpenAmount = math.Round(got.SharpenAmount*10) / 10 // the slider steps in floats; Save rounds the same way
	if !reflect.DeepEqual(got, want) {
		t.Errorf("widgets after import:\n got  %+v\n want %+v", got, want)
	}
	if h.app.keepHistory.Load() || !h.app.showDebug.Load() {
		t.Error("runtime preferences (keep history, debug output) not applied")
	}
	if h.window.Canvas().Overlays().Top() == nil {
		t.Error("no confirmation shown")
	}
}
