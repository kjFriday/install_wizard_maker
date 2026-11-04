// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/wizard/setup_test.go
// About: Tests for the window-free setup helpers: LaunchTarget, quoteForShell, human, lockName.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Tests for the window-free setup helpers: LaunchTarget, quoteForShell, human, lockName.

package wizard

import (
	"testing"

	"generic-company/wizard/internal/install"
	"generic-company/wizard/internal/spec"
)

// TestLaunchTarget checks the order Launch prefers (Start-menu shortcut, desktop shortcut, the program)
// and that a program's arguments are expanded and quoted, with pythonw.exe for a Python program.
func TestLaunchTarget(t *testing.T) {
	pl := install.SandboxPlaces(`C:\sandbox`)
	dir := `C:\sandbox\Programs\Generic Company Inc\Hello`
	for _, c := range []struct {
		startMenu, desktop          bool
		program, python             string
		args                        []string
		wantFile, wantArgs, wantDir string
	}{
		// a new shortcut comes first, so the program gets its own taskbar identity
		{true, true, "hello.exe", "", nil, `C:\sandbox\Start Menu\Generic Company Inc\Hello.lnk`, "", ""},
		{false, true, "hello.exe", "", nil, `C:\sandbox\Desktop\Hello.lnk`, "", ""},
		// no shortcut: the program itself, with its arguments expanded and quoted
		{false, false, "bin/hello.exe", "", []string{"-version", `{localappdata}\Generic Company Inc\hello.db`, "{version}", ""},
			dir + `\bin\hello.exe`, `-version "C:\sandbox\LocalAppData\Generic Company Inc\hello.db" 1.2.0 ""`, dir},
		{false, false, spec.Python, `C:\Python312\pythonw.exe`, []string{`{app}\main.py`},
			`C:\Python312\pythonw.exe`, `"` + dir + `\main.py"`, dir},
	} {
		s := &Setup{M: &spec.Manifest{Name: "Hello", Version: "1.2.0", Group: "Generic Company Inc",
			Run: spec.Run{Program: c.program, Args: c.args}}, Places: pl, Python: c.python,
			Choices: install.Choices{Dir: dir, StartMenu: c.startMenu, Desktop: c.desktop}}
		if file, args, in := s.LaunchTarget(); file != c.wantFile || args != c.wantArgs || in != c.wantDir {
			t.Errorf("%+v: got %q %q %q", c, file, args, in)
		}
	}
}

// TestQuoteForShell pins the quoting as it is, the trailing-backslash case included (see the known
// limitation on quoteForShell).
func TestQuoteForShell(t *testing.T) {
	for in, want := range map[string]string{
		"":                 `""`,
		"-version":         `-version`,
		`C:\Apps\hello`:    `C:\Apps\hello`,
		`C:\My Apps\hello`: `"C:\My Apps\hello"`,
		"tab\there":        "\"tab\there\"",
		`a"b`:              `"a\"b"`,
		`say "hi"`:         `"say \"hi\""`,
		`C:\My Apps\`:      `"C:\My Apps\"`, // a trailing backslash stays single
	} {
		if got := quoteForShell(in); got != want {
			t.Errorf("quoteForShell(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestHuman checks each unit's edges: KB rounds up, and the switch to MB and GB is at exactly 1 MiB
// and 1 GiB.
func TestHuman(t *testing.T) {
	for n, want := range map[int64]string{
		0:         "0 KB",
		1:         "1 KB",
		1024:      "1 KB",
		1025:      "2 KB",
		1<<20 - 1: "1024 KB",
		1 << 20:   "1.0 MB",
		7 << 19:   "3.5 MB",
		1<<30 - 1: "1024.0 MB",
		1 << 30:   "1.0 GB",
		5 << 29:   "2.5 GB",
	} {
		if got := human(n); got != want {
			t.Errorf("human(%d) = %q, want %q", n, got, want)
		}
	}
}

// Every setup and uninstaller a maker has ever built takes this lock, so the name must never change.
func TestLockName(t *testing.T) {
	if got := lockName("GenericCompany.Hello"); got != "GenericCompany.Wizard.GenericCompany.Hello" {
		t.Fatalf("lockName = %q", got)
	}
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=81ac403a00e8d63bb980d83be55a67284d36b4dc4928ef51ad95ff180538661c ====
