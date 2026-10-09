// portable.go — Portable Mode: settings kept beside GoVid.exe.
//
// Responsibilities:
//   - fileStore: a fyne.Preferences backed by settings.json, written
//     atomically (writeFileAtomic) a moment after the last change, and at
//     once on Flush.
//   - chooseSettingsStore: GoVid runs in portable mode when a GoVid.portable
//     marker file is beside the executable. The choice has to be made
//     before any setting is read, hence a marker rather than a setting.
//     When that folder cannot be written to, GoVid falls back to the Fyne
//     store in the user profile and says so.
//   - DownloaderApp.setPortable: Preferences → Portable Mode: creates or
//     removes the marker, copies the settings and presets into or out of
//     settings.json, and offers to restart.
//
// Download history, the saved queue, and the tools in bin/ already live
// beside the executable, so a portable folder carries everything.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
)

// The files of portable mode, beside the executable.
const (
	portableMarker   = "GoVid.portable"
	settingsFileName = "settings.json"
)

// fileStoreSaveDelay is how long fileStore waits after a change before
// writing, so a burst of changes (Save writes every key) is one write.
const fileStoreSaveDelay = 300 * time.Millisecond

// fileStore is a fyne.Preferences kept in a JSON file. Values are stored as
// JSON numbers, booleans, strings, and lists of them.
type fileStore struct {
	path  string
	write func(path string, data []byte) error // writeFileAtomic; replaced in tests

	// writeMu is held by Flush from taking the values until the file is
	// written, so two flushes cannot write out of order: without it, an
	// older snapshot written last would undo the newer one.
	writeMu sync.Mutex

	mu        sync.Mutex
	values    map[string]any
	timer     *time.Timer // pending save, or nil
	listeners []func()
	onError   func(error) // reports a failed save; may be nil
}

var _ fyne.Preferences = (*fileStore)(nil)

// newFileStore returns a store for path, read from it when it exists. A
// file that cannot be read or parsed gives an empty store and an error;
// it is replaced at the next save.
func newFileStore(path string) (*fileStore, error) {
	store := &fileStore{path: path, write: writeFileAtomic, values: map[string]any{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return store, err
	}
	if err := json.Unmarshal(data, &store.values); err != nil {
		store.values = map[string]any{}
		return store, fmt.Errorf("reading %s: %w", filepath.Base(path), err)
	}
	return store, nil
}

// Flush writes the settings now, if a save is pending. It may run on the
// save timer's goroutine and on the caller's at once (quitting, say), so it
// holds writeMu throughout: flushes write one at a time, in the order they
// took the values, and the file ends up with the latest.
func (store *fileStore) Flush() error {
	store.writeMu.Lock()
	defer store.writeMu.Unlock()
	store.mu.Lock()
	if store.timer == nil {
		store.mu.Unlock()
		return nil
	}
	store.timer.Stop()
	store.timer = nil
	data, err := json.MarshalIndent(store.values, "", "  ")
	store.mu.Unlock()
	if err != nil {
		return err
	}
	return store.write(store.path, append(data, '\n'))
}

// flushInBackground is the delayed save.
func (store *fileStore) flushInBackground() {
	if err := store.Flush(); err != nil && store.onError != nil {
		store.onError(err)
	}
}

// set stores value under key, schedules a save, and tells the listeners.
func (store *fileStore) set(key string, value any) {
	store.mu.Lock()
	store.values[key] = value
	if store.timer == nil {
		store.timer = time.AfterFunc(fileStoreSaveDelay, store.flushInBackground)
	}
	listeners := slices.Clone(store.listeners)
	store.mu.Unlock()
	for _, listener := range listeners {
		listener()
	}
}

// get returns the value under key.
func (store *fileStore) get(key string) (any, bool) {
	store.mu.Lock()
	defer store.mu.Unlock()
	value, ok := store.values[key]
	return value, ok
}

func (store *fileStore) Bool(key string) bool { return store.BoolWithFallback(key, false) }

func (store *fileStore) BoolWithFallback(key string, fallback bool) bool {
	if value, ok := store.get(key); ok {
		if b, ok := value.(bool); ok {
			return b
		}
	}
	return fallback
}

func (store *fileStore) SetBool(key string, value bool) { store.set(key, value) }

func (store *fileStore) Float(key string) float64 { return store.FloatWithFallback(key, 0) }

func (store *fileStore) FloatWithFallback(key string, fallback float64) float64 {
	if value, ok := store.get(key); ok {
		if f, ok := value.(float64); ok {
			return f
		}
	}
	return fallback
}

func (store *fileStore) SetFloat(key string, value float64) { store.set(key, value) }

func (store *fileStore) Int(key string) int { return store.IntWithFallback(key, 0) }

func (store *fileStore) IntWithFallback(key string, fallback int) int {
	if value, ok := store.get(key); ok {
		if f, ok := value.(float64); ok {
			return int(f)
		}
	}
	return fallback
}

func (store *fileStore) SetInt(key string, value int) { store.set(key, float64(value)) }

func (store *fileStore) String(key string) string { return store.StringWithFallback(key, "") }

func (store *fileStore) StringWithFallback(key, fallback string) string {
	if value, ok := store.get(key); ok {
		if s, ok := value.(string); ok {
			return s
		}
	}
	return fallback
}

func (store *fileStore) SetString(key string, value string) { store.set(key, value) }

// list returns the list stored under key, converted by convert, or
// fallback when there is none or an element has another type.
func list[T any](store *fileStore, key string, fallback []T, convert func(any) (T, bool)) []T {
	value, ok := store.get(key)
	if !ok {
		return fallback
	}
	items, ok := value.([]any)
	if !ok {
		return fallback
	}
	result := make([]T, 0, len(items))
	for _, item := range items {
		converted, ok := convert(item)
		if !ok {
			return fallback
		}
		result = append(result, converted)
	}
	return result
}

// setList stores a list under key, in the form JSON reads back.
func setList[T any](store *fileStore, key string, value []T) {
	items := make([]any, len(value))
	for i, item := range value {
		items[i] = item
	}
	store.set(key, items)
}

func toBool(value any) (bool, bool)     { b, ok := value.(bool); return b, ok }
func toFloat(value any) (float64, bool) { f, ok := value.(float64); return f, ok }
func toString(value any) (string, bool) { s, ok := value.(string); return s, ok }
func toInt(value any) (int, bool)       { f, ok := value.(float64); return int(f), ok }
func intsAsFloats(value []int) []float64 {
	return convertSlice(value, func(i int) float64 { return float64(i) })
}
func convertSlice[A, B any](in []A, f func(A) B) []B {
	out := make([]B, len(in))
	for i, item := range in {
		out[i] = f(item)
	}
	return out
}

func (store *fileStore) BoolList(key string) []bool { return store.BoolListWithFallback(key, []bool{}) }
func (store *fileStore) BoolListWithFallback(key string, fallback []bool) []bool {
	return list(store, key, fallback, toBool)
}
func (store *fileStore) SetBoolList(key string, value []bool) { setList(store, key, value) }

func (store *fileStore) FloatList(key string) []float64 {
	return store.FloatListWithFallback(key, []float64{})
}
func (store *fileStore) FloatListWithFallback(key string, fallback []float64) []float64 {
	return list(store, key, fallback, toFloat)
}
func (store *fileStore) SetFloatList(key string, value []float64) { setList(store, key, value) }

func (store *fileStore) IntList(key string) []int { return store.IntListWithFallback(key, []int{}) }
func (store *fileStore) IntListWithFallback(key string, fallback []int) []int {
	return list(store, key, fallback, toInt)
}
func (store *fileStore) SetIntList(key string, value []int) {
	setList(store, key, intsAsFloats(value))
}

func (store *fileStore) StringList(key string) []string {
	return store.StringListWithFallback(key, []string{})
}
func (store *fileStore) StringListWithFallback(key string, fallback []string) []string {
	return list(store, key, fallback, toString)
}
func (store *fileStore) SetStringList(key string, value []string) { setList(store, key, value) }

// RemoveValue deletes key.
func (store *fileStore) RemoveValue(key string) {
	store.mu.Lock()
	_, existed := store.values[key]
	delete(store.values, key)
	if existed && store.timer == nil {
		store.timer = time.AfterFunc(fileStoreSaveDelay, store.flushInBackground)
	}
	store.mu.Unlock()
}

// AddChangeListener calls listener after every change.
func (store *fileStore) AddChangeListener(listener func()) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.listeners = append(store.listeners, listener)
}

// ChangeListeners returns the change listeners.
func (store *fileStore) ChangeListeners() []func() {
	store.mu.Lock()
	defer store.mu.Unlock()
	return slices.Clone(store.listeners)
}

// ── Choosing the store ───────────────────────────────────────────────────────

// executableDir returns the folder holding GoVid's executable, "" when it
// cannot be found; a variable so tests can point it elsewhere.
var executableDir = func() string {
	exePath, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exePath)
}

// isPortable reports whether the portable marker is in exeDir.
func isPortable(exeDir string) bool {
	return exeDir != "" && fileExists(filepath.Join(exeDir, portableMarker))
}

// chooseSettingsStore returns the store the settings live in: settings.json
// in exeDir when the portable marker is there and the folder can be written
// to, else appStore's (the Fyne store in the user profile). appStore is
// only called when it is used. note explains a fallback or a damaged
// settings.json, "" otherwise.
func chooseSettingsStore(exeDir string, appStore func() fyne.Preferences, writable func(string) bool) (store fyne.Preferences, portable bool, note string) {
	if !isPortable(exeDir) {
		return appStore(), false, ""
	}
	if !writable(exeDir) {
		return appStore(), false, fmt.Sprintf("GoVid cannot write to %s, so Portable Mode is off: settings are kept in your user profile instead. Move GoVid to a folder you can write to, such as one on a USB stick or in your user folder.", exeDir)
	}
	file, err := newFileStore(filepath.Join(exeDir, settingsFileName))
	if err != nil {
		note = fmt.Sprintf("%v; starting with the default settings.", err)
	}
	return file, true, note
}

// ── Switching ────────────────────────────────────────────────────────────────

// copySettings copies the settings in use, whether or not "Save
// preferences" is on, and the presets from source's store to another store.
// The settings come from source.Load, not its store, which with "Save
// preferences" off does not hold this session's changes.
func copySettings(source *PreferenceService, to fyne.Preferences) error {
	NewPreferenceService(to).write(source.Load())
	if raw := source.store.String(prefPresets); raw != "" {
		to.SetString(prefPresets, raw)
	}
	if file, ok := to.(*fileStore); ok {
		return file.Flush()
	}
	return nil
}

// setPortable turns Portable Mode on or off for the next start, copying
// the settings to where they will be kept, and creating or removing the
// marker. It returns an error, and changes nothing, when GoVid's folder
// cannot be written to.
func (app *DownloaderApp) setPortable(on bool) error {
	exeDir := executableDir()
	if exeDir == "" {
		return errors.New("GoVid could not find its own folder")
	}
	marker := filepath.Join(exeDir, portableMarker)
	if !on {
		if err := copySettings(app.prefSvc, fyne.CurrentApp().Preferences()); err != nil {
			return err
		}
		if err := os.Remove(marker); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("removing %s: %w", portableMarker, err)
		}
		return nil
	}

	if !dirWritable(exeDir) {
		return fmt.Errorf("GoVid cannot write to %s. Move GoVid to a folder you can write to, such as one on a USB stick or in your user folder", exeDir)
	}
	file, err := newFileStore(filepath.Join(exeDir, settingsFileName))
	if err != nil {
		// A damaged settings.json is replaced by the copy.
		file, _ = newFileStore(filepath.Join(exeDir, settingsFileName))
	}
	if err := copySettings(app.prefSvc, file); err != nil {
		return err
	}
	text := "This file makes GoVid portable: its settings are kept in settings.json beside it. Delete it (or untick Portable Mode in Preferences) to keep them in your user profile.\n"
	if err := os.WriteFile(marker, []byte(text), 0644); err != nil {
		return fmt.Errorf("creating %s: %w", portableMarker, err)
	}
	return nil
}

// onPortableChanged handles the Portable Mode toggle: it switches, then
// offers to restart, or reports why it could not and puts the toggle back.
// Must be called on the UI thread.
func (app *DownloaderApp) onPortableChanged(on bool, revert func()) {
	if on == app.portable {
		return
	}
	if err := app.setPortable(on); err != nil {
		revert()
		dialog.ShowError(err, app.window)
		return
	}
	state, where := "on", "settings.json beside GoVid"
	if !on {
		state, where = "off", "your user profile"
	}
	app.appendOutput(fmt.Sprintf("[SYSTEM] Portable Mode turned %s; settings will be kept in %s from the next start.", state, where), colSystem)
	copied := fmt.Sprintf("Your settings and presets were copied. From the next start GoVid keeps them in %s.", where)
	if app.isRunning.Load() {
		dialog.ShowInformation("Portable Mode", copied+"\n\nRestart GoVid when the download has finished.", app.window)
		return
	}
	confirm := dialog.NewConfirm("Portable Mode", copied+"\n\nRestart GoVid now?", func(restart bool) {
		if restart {
			app.restart()
		}
	}, app.window)
	confirm.SetConfirmText("Restart now")
	confirm.SetDismissText("Later")
	confirm.Show()
}

// restart starts a new GoVid and quits this one.
func (app *DownloaderApp) restart() {
	exePath, err := os.Executable()
	if err == nil {
		err = startDetached(exePath)
	}
	if err != nil {
		dialog.ShowError(fmt.Errorf("could not restart GoVid: %w; start it again yourself", err), app.window)
		return
	}
	app.Shutdown(fyne.CurrentApp().Quit)
}

// flushSettings writes settings.json now, in Portable Mode, so a change
// made just before quitting is not lost.
func (app *DownloaderApp) flushSettings() {
	file, ok := app.settingsStore.(*fileStore)
	if !ok {
		return
	}
	if err := file.Flush(); err != nil {
		app.logSvc.WriteToFile(fmt.Sprintf("[SYSTEM] Could not save %s: %v", settingsFileName, err))
	}
}

// reportSettingsLocation logs where the settings are kept, and why when
// Portable Mode could not be used.
func (app *DownloaderApp) reportSettingsLocation() {
	if app.portable {
		app.appendOutput("[SYSTEM] Portable Mode: settings are kept in "+settingsFileName+" beside GoVid.", colSystem)
	}
	if app.settingsNote != "" {
		app.appendOutput("[WARNING] "+app.settingsNote, colWarning)
	}
	if file, ok := app.settingsStore.(*fileStore); ok {
		file.onError = func(err error) {
			app.appendOutput(fmt.Sprintf("[WARNING] Could not save %s: %v", settingsFileName, err), colWarning)
		}
	}
}
