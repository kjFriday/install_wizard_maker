// ==== Generic Company Inc. portfolio ====================================
// Project: install_wizard_maker | File: internal/python/python_test.go
// About: ==== Generic Company Inc. portfolio ===============================================...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// ==== Generic Company Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/python/python_test.go
// About: Tests for package python.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

package python

import (
	"os"
	"strings"
	"testing"
)

type fake map[string]string // "root|path|name" -> value; "root|path" -> subkeys (comma-separated)

func (f fake) Subkeys(root uintptr, path string) ([]string, error) {
	v, ok := f[key(root, path, "")]
	if !ok || v == "" {
		return nil, nil
	}
	return strings.Split(v, ","), nil
}

func (f fake) String(root uintptr, path, name string) (string, bool) {
	v, ok := f[key(root, path, "="+name)]
	return v, ok
}

func key(root uintptr, path, name string) string {
	r := "HKCU"
	if root == LocalMachine {
		r = "HKLM"
	}
	return r + `\` + path + name
}

const core = `Software\Python\PythonCore`

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
func TestFindHere(t *testing.T) {
	got, ok := Find(Windows{}, "3.0", func(p string) bool { _, err := os.Stat(p); return err == nil })
	if !ok {
		t.Skip("no Python registered here")
	}
	if !strings.EqualFold(got.Pythonw[len(got.Pythonw)-len("pythonw.exe"):], "pythonw.exe") {
		t.Fatalf("%+v", got)
	}
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=9fe1203c905a7f41d5a2fd267ac737b315dcabc69f67e0eb72782ad163966263 ====
