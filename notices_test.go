package main

import (
	"testing"

	"fyne.io/fyne/v2/test"
)

func TestNoticeActionRunsAndDismisses(t *testing.T) {
	mgr := newLogTestManager(t)
	ran := 0

	mgr.showNotice(notice{id: "a", text: "first", actionLabel: "Do it", action: func() { ran++ }})
	mgr.showNotice(notice{id: "a", text: "replaced", actionLabel: "Do it", action: func() { ran++ }})
	mgr.showNotice(notice{id: "b", text: "second"})

	if len(mgr.notices) != 2 || mgr.notices[0].text != "replaced" || len(mgr.noticeBox.Objects) != 2 {
		t.Fatalf("notices = %+v (%d shown), want a (replaced) and b", mgr.notices, len(mgr.noticeBox.Objects))
	}

	test.Tap(findButton(t, mgr.noticeBox, "Do it"))

	if ran != 1 {
		t.Errorf("action ran %d times, want 1", ran)
	}
	if len(mgr.notices) != 1 || mgr.notices[0].id != "b" {
		t.Errorf("notices = %+v, want only b after acting on a", mgr.notices)
	}

	// Rebuilding the window keeps the remaining notice.
	mgr.createUI()
	if len(mgr.noticeBox.Objects) != 1 {
		t.Errorf("notice area shows %d notices after createUI, want 1", len(mgr.noticeBox.Objects))
	}

	mgr.dismissNotice("b")
	if len(mgr.notices) != 0 || len(mgr.noticeBox.Objects) != 0 {
		t.Errorf("notices = %+v, want none after dismissing b", mgr.notices)
	}
}
