// ==== Generic Company Inc. portfolio ====================================
// Project: install_wizard_maker | File: internal/install/uninstall_test.go
// About: ==== Generic Company Inc. portfolio ===============================================...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// ==== Generic Company Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/install/uninstall_test.go
// About: Tests for package install.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

package install

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestUninstall(t *testing.T) {
	s := newSandbox(t)
	s.install(t, "1.0.0", v1, Choices{StartMenu: true, Desktop: true})
	os.WriteFile(filepath.Join(s.dir(), "README.md"), []byte("my notes"), 0o644) // changed
	os.WriteFile(filepath.Join(s.dir(), "notes.txt"), []byte("mine"), 0o644)     // the user's own
	purge := filepath.Join(s.pl.LocalAppData, "Generic Company Inc", "GenericCompany.Hello")
	os.MkdirAll(purge, 0o755)
	os.WriteFile(filepath.Join(purge, "settings.json"), []byte("{}"), 0o644)
	shared := filepath.Join(s.pl.LocalAppData, "Generic Company Inc", "generic_company.db")
	os.WriteFile(shared, []byte("db"), 0o644)
	m, _ := ReadMarker(s.dir())
	self := filepath.Join(s.dir(), "uninstall.exe")
	res, err := Uninstall(s.dir(), m, UEnv{Places: s.pl, Registry: s.reg, Self: self})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Kept, []string{"README.md"}) {
		t.Fatalf("kept %v", res.Kept)
	}
	if !reflect.DeepEqual(res.AfterExit, []string{self, s.dir(), filepath.Dir(s.dir())}) {
		t.Fatalf("after exit %v", res.AfterExit)
	}
	var left []string
	filepath.WalkDir(s.pl.Root, func(p string, d fs.DirEntry, err error) error {
		if !d.IsDir() {
			rel, _ := filepath.Rel(s.pl.Root, p)
			left = append(left, filepath.ToSlash(rel))
		}
		return nil
	})
	want := []string{"LocalAppData/Generic Company Inc/GenericCompany.Hello/settings.json", "LocalAppData/Generic Company Inc/generic_company.db",
		"Programs/Generic Company Inc/Generic Company Hello/README.md", "Programs/Generic Company Inc/Generic Company Hello/notes.txt",
		"Programs/Generic Company Inc/Generic Company Hello/uninstall.exe"}
	if !reflect.DeepEqual(left, want) {
		t.Fatalf("left %v", left)
	}
	if _, err := os.Stat(filepath.Join(s.pl.Root, "Start Menu", "Generic Company Inc")); !errors.Is(err, fs.ErrNotExist) {
		t.Error("empty Start-menu group left behind")
	}
	if _, ok := s.reg.keys["GenericCompany.Hello"]; ok {
		t.Error("registry entry left")
	}
	// purge removes only the program's own settings, never the shared database
	os.RemoveAll(s.dir())
	s.install(t, "1.0.0", v1, Choices{StartMenu: true})
	m, _ = ReadMarker(s.dir())
	if _, err := Uninstall(s.dir(), m, UEnv{Places: s.pl, Registry: s.reg, Self: self, Purge: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(purge); !errors.Is(err, fs.ErrNotExist) {
		t.Error("purge folder left")
	}
	if _, err := os.Stat(shared); err != nil {
		t.Error("the shared database was removed")
	}
}

func TestUninstallTouchesOnlyItsOwn(t *testing.T) {
	s := newSandbox(t)
	outside := filepath.Join(s.pl.Root, "outside.txt")
	os.WriteFile(outside, []byte("x"), 0o644)
	elsewhere := filepath.Join(s.pl.Root, "elsewhere.lnk")
	os.WriteFile(elsewhere, []byte("x"), 0o644)
	dir := s.dir()
	os.MkdirAll(dir, 0o755)
	m := &Marker{AppID: "GenericCompany.Hello", Files: []string{"../../../outside.txt", `C:\Windows\win.ini`, "www"},
		Shortcuts: []string{elsewhere, `C:\Windows\notepad.exe`},
		SHA256:    map[string]string{"../../../outside.txt": hash([]byte("x"))}}
	if _, err := Uninstall(dir, m, UEnv{Places: s.pl, Registry: s.reg}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{outside, elsewhere} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s was removed", p)
		}
	}
}

// A file in use is retried until it is free, and only then reported.
func TestRemoveWait(t *testing.T) {
	defer func(d time.Duration) { lockWait = d }(lockWait)
	file := filepath.Join(t.TempDir(), "wizard-maker.exe")
	os.WriteFile(file, []byte("x"), 0o644)
	f, err := os.Open(file) // held without delete sharing, as a running exe is
	if err != nil {
		t.Fatal(err)
	}
	lockWait = 300 * time.Millisecond
	if err := removeWait(file); err == nil {
		f.Close()
		t.Fatal("removed a file in use")
	}
	lockWait = 10 * time.Second
	go func() {
		time.Sleep(500 * time.Millisecond)
		f.Close()
	}()
	if err := removeWait(file); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("not removed")
	}
	if err := removeWait(file); err != nil {
		t.Fatalf("already gone: %v", err)
	}
}

func TestDeleteAfterExit(t *testing.T) {
	if testing.Short() {
		t.Skip("runs cmd.exe")
	}
	dir := filepath.Join(t.TempDir(), "sand box & 100%")
	sub := filepath.Join(dir, "a (1)")
	os.MkdirAll(sub, 0o755)
	file := filepath.Join(sub, "un & install%PATH%.exe")
	os.WriteFile(file, []byte("x"), 0o644)
	if err := DeleteAfterExit(file, sub, dir); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("not removed")
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=89faaef37935c5517a289777d106ce1149522a41bf415f58f4d30a58eae87f3d ====
