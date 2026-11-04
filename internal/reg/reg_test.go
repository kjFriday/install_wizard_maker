// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/reg/reg_test.go
// About: Tests for package reg: both registries on one round trip, and the sandbox format.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Tests for package reg: both registries on one round trip, and the sandbox format.
//
// TestHKCURoundTrip writes the real registry under a throwaway key below HKEY_CURRENT_USER\Software
// and removes it afterwards; -short skips it.

package reg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"generic-company/wizard/internal/install"
)

// values is a typical Apps & features entry, sorted by name as Read returns it. The
// UninstallString holds quotes, a space, "&" and "%" to show they are stored as is.
var values = []install.Value{
	install.Str("DisplayName", "Generic Company Hello 1.0.0"),
	install.DWord("EstimatedSize", 42),
	install.Str("InstallLocation", `C:\Users\x\AppData\Local\Programs\Generic Company Inc\Generic Company Hello`),
	install.DWord("NoModify", 1),
	install.Str("UninstallString", `"C:\x\uninstall.exe" -root "C:\sand box & 100%"`),
}

// roundTrip checks the install.Registry contract on r: a fresh key is not found, Write then Read
// gives the same values, a second Write replaces rather than merges, Delete works twice, and a
// deleted key is not found.
func roundTrip(t *testing.T, r install.Registry, id string) {
	t.Helper()
	if _, found, err := r.Read(id); err != nil || found {
		t.Fatalf("fresh key found: %v", err)
	}
	if err := r.Write(id, values); err != nil {
		t.Fatal(err)
	}
	got, found, err := r.Read(id)
	if err != nil || !found || !reflect.DeepEqual(got, values) {
		t.Fatalf("read back %v %v\n%+v", found, err, got)
	}
	if err := r.Write(id, values[:2]); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := r.Read(id); len(got) != 2 {
		t.Fatalf("Write did not replace the values: %+v", got)
	}
	if err := r.Delete(id); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete(id); err != nil {
		t.Fatalf("second delete: %v", err)
	}
	if _, found, _ := r.Read(id); found {
		t.Fatal("deleted key still there")
	}
}

// TestFileRoundTrip runs the round trip on a registry.json that does not exist yet.
func TestFileRoundTrip(t *testing.T) {
	roundTrip(t, File{Path: filepath.Join(t.TempDir(), "registry.json")}, "GenericCompany.Hello")
}

// The other Generic Company programs read and write the same file.
//
// It reads an entry written in the siblings' format, then writes one and checks the JSON names and
// value types, and that the sibling's entry survived.
func TestFileMatchesTheSiblings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	sibling := `{"HKCU:\\Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\GenericCompany.CMS": {"DisplayName": "Generic Company Journal 0.2.0", "NoModify": 1}}`
	os.WriteFile(path, []byte(sibling), 0o644)
	f := File{Path: path}
	got, found, err := f.Read("GenericCompany.CMS")
	if err != nil || !found || !reflect.DeepEqual(got, []install.Value{install.Str("DisplayName", "Generic Company Journal 0.2.0"), install.DWord("NoModify", 1)}) {
		t.Fatalf("%v %v %+v", found, err, got)
	}
	if err := f.Write("GenericCompany.Hello", values); err != nil {
		t.Fatal(err)
	}
	var data map[string]map[string]any
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &data); err != nil {
		t.Fatal(err)
	}
	key := data[`HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\GenericCompany.Hello`]
	if key["NoModify"] != float64(1) || key["DisplayName"] != "Generic Company Hello 1.0.0" || data[`HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\GenericCompany.CMS`] == nil {
		t.Fatalf("%s", b)
	}
}

// TestHKCURoundTrip runs the round trip on the real registry under a key named after the process
// and time, so parallel runs do not meet, then checks Subkeys and String there, including that a
// DWORD does not read as a string.
func TestHKCURoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("writes the real registry (a throwaway key)")
	}
	base := fmt.Sprintf(`Software\Generic Company Wizard Maker test\%d-%d`, os.Getpid(), time.Now().UnixNano())
	defer func() {
		HKCU{}.deleteTree(base)
		DeleteKey(CurrentUser, `Software\Generic Company Wizard Maker test`)
	}()
	roundTrip(t, HKCU{Base: base}, "GenericCompany.Hello")
	if subs, err := Subkeys(CurrentUser, base); err != nil || len(subs) != 0 {
		t.Fatalf("left %v %v", subs, err)
	}
	HKCU{Base: base}.Write("GenericCompany.Hello", values)
	if subs, _ := Subkeys(CurrentUser, base); !reflect.DeepEqual(subs, []string{"GenericCompany.Hello"}) {
		t.Fatalf("subkeys %v", subs)
	}
	if s, ok := String(CurrentUser, base+`\GenericCompany.Hello`, "DisplayName"); !ok || s != "Generic Company Hello 1.0.0" {
		t.Fatalf("String: %q %v", s, ok)
	}
	if _, ok := String(CurrentUser, base+`\GenericCompany.Hello`, "NoModify"); ok {
		t.Fatal("a DWORD read as a string")
	}
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=58ef0437462241144374e43acb506395cad7f30c68101c59d5a29f75d7be108d ====
