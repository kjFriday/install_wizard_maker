// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/shell/shell_windows_test.go
// About: Tests for package shell against the real Windows shell.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Tests for package shell against the real Windows shell.
//
// They pin struct layouts, the known folders and a .lnk round trip. They need Windows and write
// only under the test's temp folder.

package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"generic-company/wizard/internal/install"
)

// TestSizes pins the hand-written structs to the Windows sizes and MustGUID's byte order, using
// CLSID_ShellLink, whose Data4 starts with C0 and ends with 46.
func TestSizes(t *testing.T) {
	if unsafe.Sizeof(propVariant{}) != 24 || unsafe.Sizeof(GUID{}) != 16 || unsafe.Sizeof(propertyKey{}) != 20 {
		t.Fatalf("PROPVARIANT %d, GUID %d, PROPERTYKEY %d", unsafe.Sizeof(propVariant{}), unsafe.Sizeof(GUID{}), unsafe.Sizeof(propertyKey{}))
	}
	g := MustGUID("{00021401-0000-0000-C000-000000000046}")
	if g.Data1 != 0x00021401 || g.Data4 != [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46} {
		t.Fatalf("%+v", g)
	}
}

// TestUserPlaces checks the known folders against LOCALAPPDATA and the Start menu's usual suffix;
// Desktop and AppData may be redirected, so they only have to be non-empty.
func TestUserPlaces(t *testing.T) {
	pl, err := UserPlaces()
	if err != nil {
		t.Fatal(err)
	}
	local := os.Getenv("LOCALAPPDATA")
	if !strings.EqualFold(pl.Programs, filepath.Join(local, "Programs")) || !strings.EqualFold(pl.LocalAppData, local) ||
		!strings.HasSuffix(strings.ToLower(pl.StartMenu), `\start menu\programs`) || pl.Desktop == "" || pl.AppData == "" {
		t.Fatalf("%+v", pl)
	}
}

// Writes a real .lnk in a temp folder and reads every field back, the AppUserModelID too.
//
// The folder name has a space, "&" and "%" (characters cmd and environment expansion treat
// specially), the shortcut goes in folders that do not exist yet, and a second shortcut without an
// id must read back with none.
func TestShortcutRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sand box & 100%")
	os.MkdirAll(dir, 0o755)
	target := filepath.Join(dir, "hello.exe")
	os.WriteFile(target, []byte("MZ"), 0o755)
	want := install.Shortcut{Path: filepath.Join(dir, "Start Menu", "Generic Company Inc", "Generic Company Hello.lnk"), Target: target,
		Args: `--db "C:\a b\generic_company.db"`, WorkDir: dir, Icon: filepath.Join(dir, "app.ico"),
		Description: "Generic Company Hello 1.0.0", AppUserModelID: "GenericCompany.Hello"}
	if err := MakeLinkDir(want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadLink(want.Path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("read back\n%+v\nwant\n%+v", got, want)
	}
	plain := want
	plain.Path, plain.AppUserModelID = filepath.Join(dir, "plain.lnk"), ""
	if err := MakeLinkDir(plain); err != nil {
		t.Fatal(err)
	}
	if got, _ := ReadLink(plain.Path); got.AppUserModelID != "" {
		t.Fatalf("unexpected id %q", got.AppUserModelID)
	}
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=8f8f6c2f3949214fadf023bc36553ed2fa3361a60dfccbc990e5f00ce3190b89 ====
