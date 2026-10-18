package wizard

import (
	"reflect"
	"testing"
)

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
