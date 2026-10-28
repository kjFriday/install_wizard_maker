// ==== Generic Company Inc. portfolio ====================================
// Project: install_wizard_maker | File: internal/install/apply_test.go
// About: ==== Generic Company Inc. portfolio ===============================================...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// ==== Generic Company Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/install/apply_test.go
// About: Tests for package install.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeLink writes a text file where the .lnk would go.
func fakeLink(s Shortcut) error {
	b, _ := json.Marshal(s)
	return os.WriteFile(s.Path, b, 0o644)
}

func (s *sandbox) env(src memSource) Env {
	return Env{Places: s.pl, Registry: s.reg, Link: fakeLink, Source: src, Icon: []byte("ICON"),
		Uninstaller: []byte("MZ uninstaller"), Retry: time.Millisecond}
}

func (s *sandbox) install(t *testing.T, version string, src memSource, c Choices) Result {
	t.Helper()
	old, _ := ReadMarker(s.dir())
	c.Dir = s.dir()
	p, err := MakePlan(manifest(version, src), s.pl, c, old, today, 14)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Apply(p, s.env(src))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// snapshot is every file under the sandbox with its hash, plus the registry.
func (s *sandbox) snapshot(t *testing.T) string {
	t.Helper()
	var lines []string
	filepath.WalkDir(s.pl.Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(s.pl.Root, p)
		if d.IsDir() {
			lines = append(lines, rel+"/")
			return nil
		}
		b, _ := os.ReadFile(p)
		lines = append(lines, rel+" "+hash(b))
		return nil
	})
	return strings.Join(lines, "\n") + "\n" + s.reg.dump()
}

func TestApplyInstallsEverything(t *testing.T) {
	s := newSandbox(t)
	s.install(t, "1.0.0", v1, Choices{StartMenu: true, Desktop: true})
	for name, body := range v1 {
		if b, err := os.ReadFile(filepath.Join(s.dir(), filepath.FromSlash(name))); err != nil || !bytes.Equal(b, body) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(s.dir(), "app.ico")); string(b) != "ICON" {
		t.Error("app.ico")
	}
	if b, _ := os.ReadFile(filepath.Join(s.dir(), "uninstall.exe")); string(b) != "MZ uninstaller" {
		t.Error("uninstall.exe")
	}
	m, err := ReadMarker(s.dir())
	if err != nil || m.AppID != "GenericCompany.Hello" || m.Version != "1.0.0" || m.SHA256["app.ico"] != hash([]byte("ICON")) ||
		m.Installer != "Generic Company Wizard Maker test" || len(m.Shortcuts) != 2 {
		t.Fatalf("marker %+v %v", m, err)
	}
	for _, l := range m.Shortcuts {
		if _, err := os.Stat(l); err != nil {
			t.Errorf("shortcut %s: %v", l, err)
		}
	}
	if _, err := os.Stat(filepath.Join(s.dir(), ".setup")); !errors.Is(err, fs.ErrNotExist) {
		t.Error(".setup left behind")
	}
	if v, ok := Lookup(s.reg.keys["GenericCompany.Hello"], "DisplayVersion"); !ok || v.Text != "1.0.0" {
		t.Error("registry")
	}
	// the siblings' Go marker type reads it: files stay a list of strings
	var sibling struct {
		AppID     string   `json:"app_id"`
		Files     []string `json:"files"`
		Shortcuts []string `json:"shortcuts"`
	}
	b, _ := os.ReadFile(filepath.Join(s.dir(), "install.json"))
	if err := json.Unmarshal(b, &sibling); err != nil || sibling.AppID != "GenericCompany.Hello" || len(sibling.Files) != 6 {
		t.Fatalf("sibling view: %v %+v", err, sibling)
	}
}

func TestApplyUpgrade(t *testing.T) {
	s := newSandbox(t)
	s.install(t, "1.0.0", v1, Choices{StartMenu: true, Desktop: true})
	settings := filepath.Join(s.dir(), "data", "settings.json")
	os.WriteFile(settings, []byte(`{"v":1,"mine":true}`), 0o644) // the user changed it
	res := s.install(t, "2.0.0", v2, Choices{StartMenu: true})
	if !reflect.DeepEqual(res.Kept, []string{"data/settings.json"}) {
		t.Fatalf("kept %v", res.Kept)
	}
	if _, err := os.Stat(filepath.Join(s.dir(), "data", "old.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Error("data/old.json (dropped, unchanged) should be gone")
	}
	if b, _ := os.ReadFile(settings); string(b) != `{"v":1,"mine":true}` {
		t.Error("the user's settings.json was touched")
	}
	if b, _ := os.ReadFile(filepath.Join(s.dir(), "hello.exe")); string(b) != "MZ hello 2" {
		t.Error("hello.exe not upgraded")
	}
	if _, err := os.Stat(s.pl.DesktopLink("Generic Company Hello")); !errors.Is(err, fs.ErrNotExist) {
		t.Error("desktop shortcut should be gone")
	}
	m, _ := ReadMarker(s.dir())
	if m.Version != "2.0.0" || len(m.Shortcuts) != 1 {
		t.Fatalf("marker %+v", m)
	}
	// same version again is a repair
	s.install(t, "2.0.0", v2, Choices{StartMenu: true})
}

// A failure at any step leaves exactly what was there before.
func TestApplyRollsBackAtEveryStep(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		var steps []string
		record := newSandbox(t)
		if upgrade {
			record.install(t, "1.0.0", v1, Choices{StartMenu: true, Desktop: true})
		}
		plan := func(s *sandbox) *Plan {
			old, _ := ReadMarker(s.dir())
			p, err := MakePlan(manifest("2.0.0", v2), s.pl, Choices{Dir: s.dir(), StartMenu: true}, old, today, 14)
			if err != nil {
				t.Fatal(err)
			}
			return p
		}
		env := record.env(v2)
		env.Fault = func(step string) error { steps = append(steps, step); return nil }
		if _, err := Apply(plan(record), env); err != nil {
			t.Fatal(err)
		}
		if len(steps) < 8 {
			t.Fatalf("only %d steps: %v", len(steps), steps)
		}
		for _, failAt := range steps {
			s := newSandbox(t)
			if upgrade {
				s.install(t, "1.0.0", v1, Choices{StartMenu: true, Desktop: true})
			}
			before := s.snapshot(t)
			env := s.env(v2)
			env.Fault = func(step string) error {
				if step == failAt {
					return errors.New("injected")
				}
				return nil
			}
			if _, err := Apply(plan(s), env); err == nil || err.Error() != "injected" {
				t.Fatalf("upgrade=%v %s: %v", upgrade, failAt, err)
			}
			if after := s.snapshot(t); after != before {
				t.Fatalf("upgrade=%v, failing at %s changed things:\nbefore\n%s\nafter\n%s", upgrade, failAt, before, after)
			}
		}
	}
}

func TestApplyRegistryFailureRollsBack(t *testing.T) {
	s := newSandbox(t)
	s.install(t, "1.0.0", v1, Choices{StartMenu: true})
	before := s.snapshot(t)
	s.reg.fail = true
	old, _ := ReadMarker(s.dir())
	p, _ := MakePlan(manifest("2.0.0", v2), s.pl, Choices{Dir: s.dir(), StartMenu: true}, old, today, 14)
	if _, err := Apply(p, s.env(v2)); err == nil {
		t.Fatal("no error")
	}
	if s.snapshot(t) != before {
		t.Fatal("not rolled back")
	}
}

func TestApplyCancel(t *testing.T) {
	s := newSandbox(t)
	env := s.env(v1)
	n := 0
	env.Cancelled = func() bool { n++; return n > 2 }
	p, _ := MakePlan(manifest("1.0.0", v1), s.pl, Choices{Dir: s.dir(), StartMenu: true}, nil, today, 14)
	if _, err := Apply(p, env); !errors.Is(err, ErrCancelled) {
		t.Fatalf("%v", err)
	}
	if entries, _ := os.ReadDir(s.pl.Root); len(entries) != 0 {
		t.Fatalf("left %v", entries)
	}
}

func TestApplyRefusals(t *testing.T) {
	s := newSandbox(t)
	s.install(t, "1.0.0", v1, Choices{StartMenu: true})
	old, _ := ReadMarker(s.dir())
	plan := func() *Plan {
		p, err := MakePlan(manifest("2.0.0", v2), s.pl, Choices{Dir: s.dir(), StartMenu: true}, old, today, 14)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	var r *Refusal
	before := s.snapshot(t)
	// a file the program holds open
	exe := filepath.Join(s.dir(), "hello.exe")
	p16, _ := syscall.UTF16PtrFromString(exe)
	h, err := syscall.CreateFile(p16, syscall.GENERIC_READ, 0, nil, syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(plan(), s.env(v2)); !errors.As(err, &r) || !r.Retry || !strings.Contains(err.Error(), "in use") {
		t.Fatalf("in use: %v", err)
	}
	syscall.CloseHandle(h)
	// a file of the user's where the new version puts one
	stranger := filepath.Join(s.dir(), "data", "new.json")
	os.WriteFile(stranger, []byte("mine"), 0o644)
	if _, err := Apply(plan(), s.env(v2)); !errors.As(err, &r) || !strings.Contains(err.Error(), "in the way") {
		t.Fatalf("stranger: %v", err)
	}
	os.Remove(stranger)
	// an unfinished earlier setup
	os.Mkdir(filepath.Join(s.dir(), ".setup"), 0o755)
	if _, err := Apply(plan(), s.env(v2)); !errors.As(err, &r) || !strings.Contains(err.Error(), "did not finish") {
		t.Fatalf("leftover: %v", err)
	}
	os.Remove(filepath.Join(s.dir(), ".setup"))
	if after := s.snapshot(t); after != before {
		t.Fatalf("a refusal changed something:\nbefore\n%s\nafter\n%s", before, after)
	}
}

var v2 = memSource{"hello.exe": []byte("MZ hello 2"), "README.md": []byte("read me"), "data/new.json": []byte("[]")}

// ==== stamp v1 | author=kjFriday | hmac-sha256=857b4e841932e9a362209e7e77b4a0a2610ccdfa2c252c3a151f1f1dfd069bc8 ====
