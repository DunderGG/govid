package main

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestEntryModeSwitch(t *testing.T) {
	_ = test.NewApp()
	w := test.NewWindow(nil)

	entry := widget.NewEntry()
	box := container.NewVBox(entry)
	w.SetContent(box)
	w.Resize(fyne.NewSize(400, 300))

	sizeSingle := entry.MinSize()
	t.Logf("Single line MinSize: %v", sizeSingle)

	// Switch to multiline
	entry.MultiLine = true
	entry.SetMinRowsVisible(4)
	entry.SetPlaceHolder("One URL per line...")
	entry.Refresh()

	sizeMulti := entry.MinSize()
	t.Logf("Multi line MinSize: %v", sizeMulti)

	if sizeMulti.Height <= sizeSingle.Height {
		t.Errorf("Expected multi-line height (%v) to be greater than single-line height (%v)", sizeMulti.Height, sizeSingle.Height)
	}

	// Switch back to single line
	entry.MultiLine = false
	entry.SetMinRowsVisible(1)
	entry.SetPlaceHolder("Single URL...")
	entry.Refresh()

	sizeSingleAgain := entry.MinSize()
	t.Logf("Single line again MinSize: %v", sizeSingleAgain)

	if sizeSingleAgain.Height != sizeSingle.Height {
		t.Errorf("Expected single-line height (%v) to match original single-line height (%v)", sizeSingleAgain.Height, sizeSingle.Height)
	}
}

func TestBatchModeTogglePreservesOutput(t *testing.T) {
	_ = test.NewApp()
	w := test.NewWindow(nil)
	app := newDownloaderApp(w)
	mgr := app.uiManager

	mgr.createUI()

	// Append some log lines
	mgr.appendLogLine("Test log line 1", nil)
	mgr.appendLogLine("Test log line 2", nil)

	if len(mgr.ui.download.logList.Objects) != 2 {
		t.Fatalf("Expected 2 log objects, got %d", len(mgr.ui.download.logList.Objects))
	}

	entrySingleSize := mgr.ui.download.entry.MinSize()

	// Enter some text across multiple lines to test single-mode truncation too
	mgr.ui.download.entry.SetText("https://example.com/video1\nhttps://example.com/video2")

	// Toggle batch mode to true using the real wired handler
	mgr.ui.download.batchMode.SetChecked(true)

	if len(mgr.ui.download.logList.Objects) != 2 {
		t.Errorf("Expected 2 log objects preserved after enabling batch mode, got %d", len(mgr.ui.download.logList.Objects))
	}

	entryMultiSize := mgr.ui.download.entry.MinSize()
	if entryMultiSize.Height <= entrySingleSize.Height {
		t.Errorf("Expected entry height to increase from %v, got %v", entrySingleSize.Height, entryMultiSize.Height)
	}

	// Toggle back to single mode using the real wired handler
	mgr.ui.download.batchMode.SetChecked(false)

	if len(mgr.ui.download.logList.Objects) != 2 {
		t.Errorf("Expected 2 log objects preserved after disabling batch mode, got %d", len(mgr.ui.download.logList.Objects))
	}
	if mgr.ui.download.entry.MinSize().Height != entrySingleSize.Height {
		t.Errorf("Expected entry height to return to %v, got %v", entrySingleSize.Height, mgr.ui.download.entry.MinSize().Height)
	}
	if mgr.ui.download.entry.Text != "https://example.com/video1" {
		t.Errorf("Expected entry text to retain first URL, got %q", mgr.ui.download.entry.Text)
	}
}

func TestClearTerminalOutputMenuItem(t *testing.T) {
	_ = test.NewApp()
	w := test.NewWindow(nil)
	app := newDownloaderApp(w)
	mgr := app.uiManager

	mgr.createMainMenu()
	mgr.createUI()

	// Append log lines
	mgr.appendLogLine("Line 1", nil)
	mgr.appendLogLine("Line 2", nil)

	if len(mgr.ui.download.logList.Objects) != 2 {
		t.Fatalf("Expected 2 log objects, got %d", len(mgr.ui.download.logList.Objects))
	}

	mainMenu := w.MainMenu()
	if mainMenu == nil {
		t.Fatal("Expected main menu to be set on window")
	}

	var clearItem *fyne.MenuItem
	for _, menu := range mainMenu.Items {
		if menu.Label == "File" {
			for _, item := range menu.Items {
				if item.Label == "Clear Terminal Output" {
					clearItem = item
					break
				}
			}
		}
	}

	if clearItem == nil {
		t.Fatal("Could not find 'Clear Terminal Output' item in File menu")
	}

	// Trigger the menu action
	clearItem.Action()

	if len(mgr.ui.download.logList.Objects) != 0 {
		t.Errorf("Expected log objects to be cleared, got %d", len(mgr.ui.download.logList.Objects))
	}
}

