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
		if dir, err = os.MkdirTemp("", "acme-wizard-test-"); err != nil {
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
