// ==== Generic Company Inc. portfolio ====================================
// Project: install_wizard_maker | File: internal/winres/pe_windows_test.go
// About: ==== Generic Company Inc. portfolio ===============================================...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// ==== Generic Company Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/winres/pe_windows_test.go
// About: Tests for package winres.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

package winres

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unsafe"

	"generic-company/wizard/internal/testexe"
)

// maker is the wizard maker's exe; its .rsrc comes from the .syso.
func maker(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(testexe.Maker(t))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestMain(m *testing.M) {
	code := m.Run()
	testexe.Cleanup()
	os.Exit(code)
}

var (
	kernel32    = syscall.NewLazyDLL("kernel32.dll")
	version     = syscall.NewLazyDLL("version.dll")
	loadLibEx   = kernel32.NewProc("LoadLibraryExW")
	freeLib     = kernel32.NewProc("FreeLibrary")
	findResEx   = kernel32.NewProc("FindResourceExW")
	sizeofRes   = kernel32.NewProc("SizeofResource")
	loadRes     = kernel32.NewProc("LoadResource")
	lockRes     = kernel32.NewProc("LockResource")
	moveMemory  = kernel32.NewProc("RtlMoveMemory")
	verInfoSize = version.NewProc("GetFileVersionInfoSizeW")
	verInfo     = version.NewProc("GetFileVersionInfoW")
	verQuery    = version.NewProc("VerQueryValueW")
)

// loaded reads one resource the way Windows does.
func loaded(t *testing.T, path string, typ, id, lang uintptr) []byte {
	t.Helper()
	p, _ := syscall.UTF16PtrFromString(path)
	h, _, err := loadLibEx.Call(uintptr(unsafe.Pointer(p)), 0, 0x2|0x20) // AS_DATAFILE | AS_IMAGE_RESOURCE
	if h == 0 {
		t.Fatalf("LoadLibraryEx: %v", err)
	}
	defer freeLib.Call(h)
	r, _, _ := findResEx.Call(h, typ, id, lang)
	if r == 0 {
		return nil
	}
	n, _, _ := sizeofRes.Call(h, r)
	g, _, _ := loadRes.Call(h, r)
	ptr, _, _ := lockRes.Call(g)
	out := make([]byte, n)
	if n > 0 {
		moveMemory.Call(uintptr(unsafe.Pointer(&out[0])), ptr, n) // copy out of the mapped file
	}
	return out
}

func versionString(t *testing.T, path, key string) string {
	t.Helper()
	p, _ := syscall.UTF16PtrFromString(path)
	n, _, _ := verInfoSize.Call(uintptr(unsafe.Pointer(p)), 0)
	if n == 0 {
		return ""
	}
	buf := make([]byte, n)
	verInfo.Call(uintptr(unsafe.Pointer(p)), 0, n, uintptr(unsafe.Pointer(&buf[0])))
	q, _ := syscall.UTF16PtrFromString(`\StringFileInfo\040904B0\` + key)
	var val *uint16
	var size uint32
	if r, _, _ := verQuery.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(q)),
		uintptr(unsafe.Pointer(&val)), uintptr(unsafe.Pointer(&size))); r == 0 || size == 0 {
		return ""
	}
	return syscall.UTF16ToString(unsafe.Slice(val, size))
}

func TestResourceSectionIsLast(t *testing.T) {
	exe := maker(t)
	h, err := parsePE(exe)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.rsrc(exe); err != nil {
		t.Fatalf("Go's linker no longer puts .rsrc last: %v", err)
	}
}

func TestSetResourcesLoadsInWindows(t *testing.T) {
	exe := maker(t)
	old, err := Resources(exe)
	if err != nil {
		t.Fatal(err)
	}
	var manifest []byte
	for _, r := range old {
		if r.Type == TypeManifest {
			manifest = r.Data
		}
	}
	if !bytes.Contains(manifest, []byte("Common-Controls")) {
		t.Fatal("the maker's manifest is missing")
	}
	// a different icon: just the 48 px image, so the group visibly changes
	ico, _ := os.ReadFile("../../icon/app.ico")
	images, _ := ParseICO(ico)
	res := append(IconResources(images[2:3], 1), Resource{Type: TypeManifest, ID: 1, Lang: LangEnUS, Data: manifest},
		VersionInfo{Version: ParseVersion("2.3.4"), Strings: [][2]string{{"ProductName", "Generic Company Hello"},
			{"FileDescription", "Generic Company Hello Setup"}}}.Resource())
	out, err := SetResources(append(exe, []byte("an old payload")...), res)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := SetResources(exe, res)
	if !bytes.Equal(out, again) {
		t.Fatal("the same resources gave different bytes")
	}
	path := filepath.Join(t.TempDir(), "setup.exe")
	if err := os.WriteFile(path, out, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := loaded(t, path, TypeIcon, 1, LangEnUS); !bytes.Equal(got, images[2].Data) {
		t.Fatalf("RT_ICON 1 is %d bytes", len(got))
	}
	if got := loaded(t, path, TypeIcon, 2, LangEnUS); got != nil {
		t.Fatal("the old RT_ICON 2 survived")
	}
	if got := loaded(t, path, TypeManifest, 1, LangEnUS); !bytes.Equal(got, manifest) {
		t.Fatal("manifest not kept")
	}
	if got := versionString(t, path, "FileDescription"); got != "Generic Company Hello Setup" {
		t.Fatalf("FileDescription %q", got)
	}
	run := exec.Command(path, "-version")
	b, err := run.Output()
	if err != nil || !strings.Contains(string(b), "Generic Company Wizard Maker") {
		t.Fatalf("rewritten exe does not run: %v %q", err, b)
	}
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=d2c961eaddb784d886a7ec3c31b3f50c4957e157c7d42b5a1938a1cf027427f4 ====
