// ==== Generic Company Inc. portfolio ====================================
// Project: install_wizard_maker | File: internal/shell/shell_test.go
// About: ==== Generic Company Inc. portfolio ===============================================...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// ==== Generic Company Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/shell/shell_test.go
// About: Tests for package shell.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"generic-company/wizard/internal/install"
)

func TestSizes(t *testing.T) {
	if unsafe.Sizeof(propVariant{}) != 24 || unsafe.Sizeof(GUID{}) != 16 || unsafe.Sizeof(propertyKey{}) != 20 {
		t.Fatalf("PROPVARIANT %d, GUID %d, PROPERTYKEY %d", unsafe.Sizeof(propVariant{}), unsafe.Sizeof(GUID{}), unsafe.Sizeof(propertyKey{}))
	}
	g := MustGUID("{00021401-0000-0000-C000-000000000046}")
	if g.Data1 != 0x00021401 || g.Data4 != [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46} {
		t.Fatalf("%+v", g)
	}
}

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

// ==== stamp v1 | author=kjFriday | hmac-sha256=dc08d28c10dc24266e0cba773c41cde0f419c9e5dcbb798ba1764d1b519d0e65 ====
