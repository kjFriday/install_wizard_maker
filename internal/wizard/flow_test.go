// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/wizard/flow_test.go
// About: Tests for Flow: the pages per option set, the walk's labels and Back, and the gates.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Tests for Flow: the pages per option set, the walk's labels and Back, and the gates.

package wizard

import (
	"reflect"
	"testing"
)

// TestSetupPages checks the optional pages, including that an upgrade skips Folder even when asked.
func TestSetupPages(t *testing.T) {
	for _, c := range []struct {
		license, folder, shortcuts, upgrade bool
		want                                []Page
	}{
		{true, true, true, false, []Page{Welcome, License, Folder, Shortcuts, Progress, Finish}},
		{true, true, true, true, []Page{Welcome, License, Shortcuts, Progress, Finish}},
		{false, false, false, false, []Page{Welcome, Progress, Finish}},
		{false, true, false, false, []Page{Welcome, Folder, Progress, Finish}},
	} {
		if got := SetupFlow(c.license, c.folder, c.shortcuts, c.upgrade).Pages(); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%+v: %v", c, got)
		}
	}
}

// TestSetupWalk walks Welcome, License, Folder, Progress, Finish: Next says Install on the page before
// the work and Finish on the last, and Back works only on the pages between the first and the work.
func TestSetupWalk(t *testing.T) {
	f := SetupFlow(true, true, false, false)
	var labels []string
	var back []bool
	for {
		labels = append(labels, f.NextLabel())
		back = append(back, f.CanBack())
		if !f.Next() {
			break
		}
	}
	if !reflect.DeepEqual(labels, []string{"&Next >", "&Next >", "&Install", "&Next >", "&Finish"}) {
		t.Fatalf("labels %v", labels)
	}
	if !reflect.DeepEqual(back, []bool{false, true, true, false, false}) {
		t.Fatalf("back %v", back)
	}
	if f.Back() || f.Page() != Finish {
		t.Fatal("went back from the finish page")
	}
}

// TestGates checks the blocked welcome page, the unaccepted license, the work page (no Next, no Back),
// and the uninstaller's labels and its banner on Done.
func TestGates(t *testing.T) {
	f := SetupFlow(true, false, false, false)
	if !f.CanNext(Gate{}) || f.CanNext(Gate{Blocked: true}) {
		t.Fatal("welcome gate")
	}
	f.Next()
	if f.Page() != License || f.CanNext(Gate{}) || !f.CanNext(Gate{Accepted: true}) {
		t.Fatal("license gate")
	}
	f.Next()
	if !f.Working() || f.CanNext(Gate{Accepted: true}) || f.CanBack() {
		t.Fatal("progress page")
	}
	u := UninstallFlow()
	if u.NextLabel() != "&Uninstall" || u.CanNext(Gate{Blocked: true}) || u.CanBack() {
		t.Fatal("uninstall confirm")
	}
	u.Next()
	u.Next()
	if u.Page() != Done || u.NextLabel() != "&Close" || !u.Page().Exterior() {
		t.Fatal("uninstall done")
	}
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=b1dfff8b182d1acbe024581e8c2764cf9dc8f84394fb162d8a9a160382b6f155 ====
