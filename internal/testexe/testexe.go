// ==== Generic Company Inc. portfolio ====================================
// Project: install_wizard_maker | File: internal/testexe/testexe.go
// About: ==== Generic Company Inc. portfolio ===============================================...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// ==== Generic Company Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/testexe/testexe.go
// About: Package testexe builds the wizard maker once for a test binary, as released (-trimpath...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Package testexe builds the wizard maker once for a test binary, as released (-trimpath, windowsgui,
// stripped), for tests that need a real exe: the program part of every setup.exe.
package testexe

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var (
	once sync.Once
	dir  string
	path string
	err  error
)

// Maker returns the path of a freshly built wizard-maker.exe. Skipped with -short.
func Maker(t testing.TB) string {
	t.Helper()
	if testing.Short() {
		t.Skip("builds and runs exes")
	}
	once.Do(func() {
		// go test caches results by the files a test opens; the exe is built from all of these
		filepath.WalkDir(moduleRoot(), func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				os.Stat(p)
			}
			return nil
		})
		if dir, err = os.MkdirTemp("", "generic-company-wizard-test-"); err != nil {
			return
		}
		path = filepath.Join(dir, "wizard-maker.exe")
		cmd := exec.Command("go", "build", "-trimpath", "-ldflags", "-H windowsgui -s -w", "-o", path, ".")
		cmd.Dir = moduleRoot()
		if out, e := cmd.CombinedOutput(); e != nil {
			err = fmt.Errorf("go build: %v: %s", e, out)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	return path
}

// Fixture copies testdata/fixture into a temp folder, with the maker itself as its hello.exe (a
// program with an icon), and returns the folder.
func Fixture(t testing.TB) string {
	t.Helper()
	exe := Maker(t)
	dir := filepath.Join(t.TempDir(), "fixture")
	if err := os.CopyFS(dir, os.DirFS(filepath.Join(moduleRoot(), "testdata", "fixture"))); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(exe)
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, "hello.exe"), b, 0o755)
	}
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// Cleanup removes the build; call it at the end of TestMain.
func Cleanup() {
	if dir != "" {
		os.RemoveAll(dir)
	}
}

func moduleRoot() string {
	out, _ := exec.Command("go", "env", "GOMOD").Output()
	return filepath.Dir(strings.TrimSpace(string(out)))
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=658bbb0f8db701d4710cae54927992979f922d6b05346efd639524e1792737d6 ====
