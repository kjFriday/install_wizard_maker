// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/python/python_test.go
// About: Tests for package python: Find on a fake and on the real registry, and parse.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Tests for package python: Find on a fake and on the real registry, and parse.

package python

import (
	"os"
	"strings"
	"testing"
)

// fake is a registry in a map, keyed by the strings key builds.
type fake map[string]string // `HKCU\path=name` -> value; `HKCU\path` -> subkeys (comma-separated)

// Subkeys returns the comma-separated list stored for the key, or none.
func (f fake) Subkeys(root uintptr, path string) ([]string, error) {
	v, ok := f[key(root, path, "")]
	if !ok || v == "" {
		return nil, nil
	}
	return strings.Split(v, ","), nil
}

// String returns the value stored for path and name ("" is the default value).
func (f fake) String(root uintptr, path, name string) (string, bool) {
	v, ok := f[key(root, path, "="+name)]
	return v, ok
}

// key is the map key for a root and path; name is "" for the subkey list or "="+value name for a
// value (so "=" alone is the default value). Any root other than LocalMachine counts as HKCU.
func key(root uintptr, path, name string) string {
	r := "HKCU"
	if root == LocalMachine {
		r = "HKLM"
	}
	return r + `\` + path + name
}

// core is CPython's PEP 514 key, the same in both roots.
const core = `Software\Python\PythonCore`

// TestFind pins Find's choice over a fake registry: per-user 3.9 (too old), 3.12 (with
// WindowedExecutablePath), 3.13t (folder only) and all-users 3.11 and 3.13 (with SysVersion and a
// trailing backslash). 3.13t wins the tie with all-users 3.13 by being per-user; without it the
// all-users 3.13 wins; nothing is found above every version, without pythonw.exe, or in an empty
// registry.
func TestFind(t *testing.T) {
	reg := fake{
		key(CurrentUser, core, ""):                                            "3.9,3.12,3.13t",
		key(CurrentUser, core+`\3.9\InstallPath`, "="):                        `C:\Py39`,
		key(CurrentUser, core+`\3.12\InstallPath`, "=WindowedExecutablePath"): `C:\Py312\pythonw.exe`,
		key(CurrentUser, core+`\3.13t\InstallPath`, "="):                      `C:\Py313t`,
		key(LocalMachine, core, ""):                                           "3.11,3.13",
		key(LocalMachine, core+`\3.11\InstallPath`, "="):                      `C:\Program Files\Python311`,
		key(LocalMachine, core+`\3.13\InstallPath`, "="):                      `C:\Program Files\Python313\`,
		key(LocalMachine, core+`\3.13`, "=SysVersion"):                        "3.13",
	}
	all := func(string) bool { return true }
	got, ok := Find(reg, "3.10", all)
	if !ok || got.Version != [2]int{3, 13} || got.Pythonw != `C:\Py313t\pythonw.exe` {
		t.Fatalf("newest (per-user wins the tie): %+v", got)
	}
	notT := func(p string) bool { return !strings.Contains(p, "313t") }
	if got, _ := Find(reg, "3.10", notT); got.Pythonw != `C:\Program Files\Python313\pythonw.exe` {
		t.Fatalf("all-users 3.13: %+v", got)
	}
	if got, _ := Find(reg, "3.12", func(p string) bool { return strings.Contains(p, "312") }); got.Tag != "3.12" {
		t.Fatalf("WindowedExecutablePath: %+v", got)
	}
	if _, ok := Find(reg, "3.14", all); ok {
		t.Fatal("found a Python newer than any registered")
	}
	if _, ok := Find(reg, "3.10", func(string) bool { return false }); ok {
		t.Fatal("found a missing pythonw.exe")
	}
	if _, ok := Find(fake{}, "3.10", all); ok {
		t.Fatal("found Python in an empty registry")
	}
}

// TestParse pins the tag forms parse accepts (plain, -32, t suffix, patch level) and the ones it
// refuses (empty, no dot, non-numeric major or minor).
func TestParse(t *testing.T) {
	for s, want := range map[string][2]int{"3.12": {3, 12}, "3.12-32": {3, 12}, "3.13t": {3, 13}, "3.10.4": {3, 10}} {
		if got, ok := parse(s); !ok || got != want {
			t.Errorf("%s: %v", s, got)
		}
	}
	for _, s := range []string{"", "3", "x.1", "3.x"} {
		if _, ok := parse(s); ok {
			t.Errorf("%q parsed", s)
		}
	}
}

// On this computer: whatever Find picks must exist.
//
// It reads the real registry with any Python 3, and skips where none is registered.
func TestFindHere(t *testing.T) {
	got, ok := Find(Windows{}, "3.0", func(p string) bool { _, err := os.Stat(p); return err == nil })
	if !ok {
		t.Skip("no Python registered here")
	}
	if !strings.EqualFold(got.Pythonw[len(got.Pythonw)-len("pythonw.exe"):], "pythonw.exe") {
		t.Fatalf("%+v", got)
	}
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=c68977a74d3f01f98aba35020442f9eb7ffb7a98b88a4824755b2f7ee19d4a83 ====
