package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

func TestFileStoreKeepsEveryType(t *testing.T) {
	path := filepath.Join(t.TempDir(), settingsFileName)
	store, err := newFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	store.SetBool("b", true)
	store.SetFloat("f", 1.5)
	store.SetInt("i", 42)
	store.SetString("s", "Hej")
	store.SetBoolList("bl", []bool{true, false})
	store.SetFloatList("fl", []float64{0.5})
	store.SetIntList("il", []int{1, 2})
	store.SetStringList("sl", []string{"a", "b"})
	store.SetString("gone", "x")
	store.RemoveValue("gone")
	if err := store.Flush(); err != nil {
		t.Fatal(err)
	}

	reread, err := newFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	switch {
	case !reread.Bool("b"), reread.Float("f") != 1.5, reread.Int("i") != 42, reread.String("s") != "Hej":
		t.Errorf("scalars did not survive: %v %v %v %q", reread.Bool("b"), reread.Float("f"), reread.Int("i"), reread.String("s"))
	case !slices.Equal(reread.BoolList("bl"), []bool{true, false}), !slices.Equal(reread.FloatList("fl"), []float64{0.5}),
		!slices.Equal(reread.IntList("il"), []int{1, 2}), !slices.Equal(reread.StringList("sl"), []string{"a", "b"}):
		t.Error("lists did not survive")
	case reread.StringWithFallback("gone", "fallback") != "fallback", reread.IntWithFallback("missing", 7) != 7:
		t.Error("a removed or missing key did not give the fallback")
	}
}

func TestFileStoreSavesShortlyAfterAChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), settingsFileName)
	store, _ := newFileStore(path)
	changes := 0
	store.AddChangeListener(func() { changes++ })

	store.SetString("format", formatMKV)
	if _, err := os.Stat(path); err == nil {
		t.Error("written at once; saves should wait a moment")
	}
	deadline := time.Now().Add(5 * time.Second)
	for !fileExists(path) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"format": "MKV"`) || changes != 1 {
		t.Errorf("settings.json = %s; %d change(s) reported", data, changes)
	}
}

func TestFileStoreStartsEmptyFromADamagedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), settingsFileName)
	os.WriteFile(path, []byte("{not json"), 0644)

	store, err := newFileStore(path)

	if err == nil || store.String("format") != "" {
		t.Errorf("newFileStore() = %v, format %q; want an error and an empty store", err, store.String("format"))
	}
}

// failingStore stands for the Fyne store in tests that must not use it.
func failingStore(t *testing.T) func() fyne.Preferences {
	return func() fyne.Preferences {
		t.Helper()
		t.Error("the Fyne store in the user profile was used")
		return test.NewApp().Preferences()
	}
}

func TestChooseSettingsStore(t *testing.T) {
	dir := t.TempDir()
	app := test.NewApp().Preferences()
	if store, portable, _ := chooseSettingsStore(dir, func() fyne.Preferences { return app }, dirWritable); portable || store != app {
		t.Error("without the marker, the user profile store was not used")
	}

	os.WriteFile(filepath.Join(dir, portableMarker), nil, 0644)
	store, portable, note := chooseSettingsStore(dir, failingStore(t), dirWritable)
	if file, ok := store.(*fileStore); !portable || !ok || file.path != filepath.Join(dir, settingsFileName) || note != "" {
		t.Errorf("with the marker: %T portable=%v note=%q, want settings.json beside GoVid", store, portable, note)
	}

	store, portable, note = chooseSettingsStore(dir, func() fyne.Preferences { return app }, func(string) bool { return false })
	if portable || store != app || !strings.Contains(note, "cannot write") {
		t.Errorf("unwritable folder: portable=%v note=%q, want the user profile store and a note", portable, note)
	}
}

// portableHarness is a download harness whose GoVid folder, exeDir, has the
// portable marker when portable is set.
func portableHarness(t *testing.T, portable bool) (*downloadHarness, string) {
	t.Helper()
	exeDir := t.TempDir()
	if portable {
		os.WriteFile(filepath.Join(exeDir, portableMarker), nil, 0644)
	}
	saved := executableDir
	executableDir = func() string { return exeDir }
	t.Cleanup(func() { executableDir = saved })
	return newDownloadHarness(t, "ytdlp-download"), exeDir
}

func TestPortableModeNeverTouchesTheUserProfileStore(t *testing.T) {
	h, exeDir := portableHarness(t, true)
	profile := fyne.CurrentApp().Preferences()
	if !h.app.portable {
		t.Fatal("the marker did not turn on Portable Mode")
	}

	h.app.ui.download.format.SetSelected(formatMKV)
	h.app.uiManager.savePreferences(h.saveDir)
	h.app.prefSvc.SavePresets([]Preset{{Name: "Mine"}})
	h.app.flushSettings()

	data, err := os.ReadFile(filepath.Join(exeDir, settingsFileName))
	if err != nil || !strings.Contains(string(data), `"format": "MKV"`) || !strings.Contains(string(data), "Mine") {
		t.Errorf("settings.json = %s, %v; want the format and the preset", data, err)
	}
	for _, key := range []string{prefFormat, prefSavedPath, prefPresets} {
		if value := profile.String(key); value != "" {
			t.Errorf("the user profile store has %s = %q", key, value)
		}
	}
}

func TestSwitchingPortableModeCopiesTheSettings(t *testing.T) {
	h, exeDir := portableHarness(t, false)
	h.app.ui.download.format.SetSelected(formatWebM)
	h.app.uiManager.savePreferences(h.saveDir)
	h.app.prefSvc.SavePresets([]Preset{{Name: "Travel"}})

	if err := h.app.setPortable(true); err != nil {
		t.Fatalf("setPortable(true) = %v", err)
	}
	if !fileExists(filepath.Join(exeDir, portableMarker)) {
		t.Fatal("no marker")
	}
	// The next start, here or on another machine with the folder copied.
	copied := t.TempDir()
	for _, name := range []string{portableMarker, settingsFileName} {
		data, _ := os.ReadFile(filepath.Join(exeDir, name))
		os.WriteFile(filepath.Join(copied, name), data, 0644)
	}
	store, portable, _ := chooseSettingsStore(copied, failingStore(t), dirWritable)
	next := NewPreferenceService(store)
	if !portable || next.Load().Format != formatWebM || len(next.LoadPresets()) != 1 || next.LoadPresets()[0].Name != "Travel" {
		t.Errorf("the copied folder starts with format %q and presets %q", next.Load().Format, presetNames(next.LoadPresets()))
	}

	// And back: settings.json's values go to the user profile store.
	h.app.settingsStore = store
	store.SetString(prefFormat, formatMP3)
	defer store.(*fileStore).Flush()
	if err := h.app.setPortable(false); err != nil {
		t.Fatalf("setPortable(false) = %v", err)
	}
	if fileExists(filepath.Join(exeDir, portableMarker)) {
		t.Error("the marker was not removed")
	}
	if got := fyne.CurrentApp().Preferences().String(prefFormat); got != formatMP3 {
		t.Errorf("user profile store has format %q, want MP3 from settings.json", got)
	}
}
