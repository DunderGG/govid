package main

import (
	"slices"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

// shortcutHarness is a download harness whose main window has its menu and
// shortcuts.
func shortcutHarness(t *testing.T) *downloadHarness {
	t.Helper()
	h := newDownloadHarness(t, "ytdlp-download")
	h.window.Resize(fyne.NewSize(900, 700))
	h.app.uiManager.createMainMenu()
	return h
}

// press types shortcut on the harness's main window, as the driver does
// when nothing claims it first.
func press(h *downloadHarness, shortcut fyne.Shortcut) {
	h.window.Canvas().(fyne.Shortcutable).TypedShortcut(shortcut)
}

// typeKey types a key without a modifier on window.
func typeKey(window fyne.Window, key fyne.KeyName) {
	if onKey := window.Canvas().OnTypedKey(); onKey != nil {
		onKey(&fyne.KeyEvent{Name: key})
	}
}

func TestShortcutsOpenTheirWindows(t *testing.T) {
	h := shortcutHarness(t)
	manager := h.app.uiManager
	// History loads in the background; hold the load until the test has
	// closed the window (see TestEscClosesTheOtherWindows).
	closed := make(chan struct{})
	defer close(closed)
	manager.onLoadHistory = func() ([]DownloadHistoryEntry, error) { <-closed; return nil, nil }
	tests := []struct {
		name   string
		open   func()
		window *fyne.Window
	}{
		{"Ctrl+H opens History", func() { press(h, shortcutHistory) }, &manager.historyWindow},
		{"Ctrl+, opens Preferences", func() { press(h, shortcutPreferences) }, &manager.prefsWindow},
		{"F1 opens the guide", func() { typeKey(h.window, fyne.KeyF1) }, &manager.helpWindow},
	}
	for _, tt := range tests {
		tt.open()
		if *tt.window == nil {
			t.Errorf("%s: the window did not open", tt.name)
			continue
		}
		typeKey(*tt.window, fyne.KeyEscape)
		if *tt.window != nil {
			t.Errorf("%s: Esc did not close it", tt.name)
		}
	}
}

func TestEscClosesTheOtherWindows(t *testing.T) {
	h := shortcutHarness(t)
	manager := h.app.uiManager
	// The windows fill themselves in from background goroutines; under the
	// test driver those run fyne.Do on their own goroutine, so they wait
	// here until the test has closed the window.
	closed := make(chan struct{})
	defer close(closed)
	manager.onYtDlpVersions = func() (string, string) { <-closed; return "1", "1" }
	manager.onJSRuntimeLabel = func() string { return "none" }
	manager.onComponents = func() []componentStatus { <-closed; return nil }
	for name, open := range map[string]func() *fyne.Window{
		"About":           func() *fyne.Window { manager.showAbout(); return &manager.aboutWindow },
		"Post-Processing": func() *fyne.Window { manager.showPostProcessing(); return &manager.ppWindow },
		"Components":      func() *fyne.Window { manager.showComponents(); return &manager.compWindow },
	} {
		window := open()
		typeKey(*window, fyne.KeyEscape)
		if *window != nil {
			t.Errorf("Esc did not close %s", name)
		}
	}
}

func TestShortcutsActOnTheMainWindow(t *testing.T) {
	h := shortcutHarness(t)
	opened := 0
	h.app.uiManager.onOpenFolder = func() { opened++ }

	press(h, shortcutOpenFolder)
	if opened != 1 {
		t.Errorf("Ctrl+O opened the folder %d times", opened)
	}

	fyne.CurrentApp().Clipboard().SetContent("https://example.com/pasted")
	press(h, shortcutPasteURLs)
	if got := h.app.ui.download.entry.Text; got != "https://example.com/pasted" {
		t.Errorf("Ctrl+Shift+V: URL field = %q", got)
	}

	press(h, shortcutLoadFile)
	if h.window.Canvas().Overlays().Top() == nil {
		t.Error("Ctrl+L did not open the file picker")
	}
	h.window.Canvas().Overlays().Top().Hide()

	h.app.ui.download.entry.SetText("https://example.com/v")
	press(h, shortcutDownload)
	waitForSessionEnd(t, h)
	if files := h.savedFiles(t); len(files) != 1 {
		t.Errorf("Ctrl+Enter downloaded %q", files)
	}
}

func TestDownloadShortcutWaitsForTheButton(t *testing.T) {
	h := shortcutHarness(t)
	h.app.ui.download.entry.SetText("https://example.com/v")
	h.app.ui.download.downloadBtn.Disable()

	press(h, shortcutDownload)

	if h.app.isRunning.Load() || h.runs() != 0 {
		t.Error("Ctrl+Enter started a download while the button was disabled")
	}
}

func TestMenusShowTheShortcuts(t *testing.T) {
	h := shortcutHarness(t)
	var shown []string
	for _, menu := range h.window.MainMenu().Items {
		for _, item := range menu.Items {
			if item.Shortcut != nil {
				shown = append(shown, item.Shortcut.ShortcutName())
			}
		}
	}
	for _, shortcut := range []*desktop.CustomShortcut{shortcutDownload, shortcutOpenFolder, shortcutLoadFile, shortcutPasteURLs, shortcutHistory, shortcutPreferences, shortcutGuide} {
		if !slices.Contains(shown, shortcut.ShortcutName()) {
			t.Errorf("no menu item shows %s", shortcut.ShortcutName())
		}
	}
}

func TestSystemThemeFollowsTheVariant(t *testing.T) {
	_ = test.NewApp()
	system := &systemTheme{}
	for _, variant := range []fyne.ThemeVariant{theme.VariantLight, theme.VariantDark} {
		var want fyne.Theme = &darkTheme{}
		if variant == theme.VariantLight {
			want = &lightTheme{}
		}
		for _, name := range []fyne.ThemeColorName{theme.ColorNameBackground, theme.ColorNameForeground, theme.ColorNamePrimary} {
			if got, expected := system.Color(name, variant), want.Color(name, variant); got != expected {
				t.Errorf("variant %v, %s: %v, want %v", variant, name, got, expected)
			}
		}
	}
	if resolveThemeMode(themeLight) != themeLight || resolveThemeMode(themeDark) != themeDark || resolveThemeMode("") != themeDark {
		t.Error("an explicit theme was not kept")
	}
	want := themeDark
	if systemVariant() == theme.VariantLight {
		want = themeLight
	}
	if got := resolveThemeMode(themeSystem); got != want {
		t.Errorf("resolveThemeMode(System) = %q with variant %v", got, systemVariant())
	}
}

func TestSystemIsTheDefaultThemeForNewInstallsOnly(t *testing.T) {
	fresh := NewPreferenceService(test.NewTempApp(t).Preferences())
	if got := fresh.Load().ThemeMode; got != themeSystem {
		t.Errorf("new install theme = %q, want System", got)
	}
	existing := test.NewTempApp(t).Preferences()
	existing.SetString(prefThemeMode, themeDark)
	if got := NewPreferenceService(existing).Load().ThemeMode; got != themeDark {
		t.Errorf("saved Dark became %q", got)
	}
}
