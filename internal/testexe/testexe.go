// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/testexe/testexe.go
// About: Package testexe builds the wizard maker once per test binary, as released, for tests.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Package testexe builds the wizard maker once per test binary, as released, for tests.
//
// The build uses the release flags (-trimpath, windowsgui, stripped), for tests that need a real
// exe: the program part of every setup.exe. The module's end-to-end tests (main_test.go), the
// maker's build tests and winres's PE tests call Maker or Fixture, and a TestMain calls Cleanup.
//
// Steps of the first Maker call:
//  1. Stat every file in the module, so go test's cache knows the result depends on them.
//  2. Run "go build" from the module root into a fresh temp folder.
//  3. Remember the path (or the error) for every later call in the same test binary.
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

// The one build per test binary: once guards it, dir is its temp folder, path the exe, and err the
// build's failure, which every later Maker call reports too.
var (
	once sync.Once
	dir  string
	path string
	err  error
)

// Maker returns the path of a freshly built wizard-maker.exe. Skipped with -short.
//
// It needs the go command on PATH. A failed build fails the calling test (and every later one)
// with the compiler's output. Safe to call from parallel tests: sync.Once makes the others wait.
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
		// -H windowsgui: a GUI exe with no console; -s -w: no symbol table and no DWARF, as released
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
//
// The folder is under t.TempDir, so the test framework removes it. Skipped with -short, like Maker.
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

// moduleRoot is the folder holding go.mod, from "go env GOMOD".
//
// Tests run in their package's folder, so this finds the module from any package. If go fails the
// output is empty and the result is ".", the current folder.
func moduleRoot() string {
	out, _ := exec.Command("go", "env", "GOMOD").Output()
	return filepath.Dir(strings.TrimSpace(string(out)))
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=311d60eec56e170555d96d4e1932f64b959c291f68f845d0855e349fd223de23 ====
