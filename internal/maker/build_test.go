// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/maker/build_test.go
// About: Tests for Build and the build command line, on a real maker exe built by testexe.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Tests for Build and the build command line, on a real maker exe built by testexe.
//
// They are slow (the exe is built once per test binary) and are skipped with -short. The fixture
// folder is testdata/fixture with the maker itself as its hello.exe, so the packed program has an
// icon of its own.

package maker

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"generic-company/wizard/internal/payload"
	"generic-company/wizard/internal/spec"
	"generic-company/wizard/internal/testexe"
	"generic-company/wizard/internal/winres"
)

// TestMain runs the tests, then removes the maker exe testexe built for them.
func TestMain(m *testing.M) {
	code := m.Run()
	testexe.Cleanup()
	os.Exit(code)
}

// build loads dir's setup.json and builds it with the test maker as the stub, logging to t. Any
// error fails the test.
func build(t *testing.T, dir string) Result {
	t.Helper()
	s, err := spec.Load(filepath.Join(dir, "setup.json"))
	if err != nil {
		t.Fatal(err)
	}
	stub, err := os.ReadFile(testexe.Maker(t))
	if err != nil {
		t.Fatal(err)
	}
	r, err := Build(s, stub, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// TestBuildMakesASetup pins the built setup.exe: its output path, a Setup payload whose manifest
// lists the three fixture files with the right sizes and hashes, packed bytes that match, the
// license text, no build-machine folder in the manifest, hello.exe's icon in the setup's resources,
// and version info 1.0.0.0 with "Wizard Fixture Setup".
func TestBuildMakesASetup(t *testing.T) {
	dir := testexe.Fixture(t)
	r := build(t, dir)
	if r.Path != filepath.Join(dir, "dist", "Wizard Fixture-1.0.0-setup.exe") || r.Files != 3 {
		t.Fatalf("%+v", r)
	}
	p, err := payload.Open(r.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if p.Kind != payload.Setup {
		t.Fatalf("kind %d", p.Kind)
	}
	b, err := p.ReadAll("setup.json", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	m, err := spec.ParseManifest(b)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, f := range m.Files {
		paths = append(paths, f.Path)
		src, _ := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f.Path)))
		sum := sha256.Sum256(src)
		if f.SHA256 != hex.EncodeToString(sum[:]) || f.Size != int64(len(src)) {
			t.Errorf("%s: manifest does not match the file", f.Path)
		}
		var out bytes.Buffer
		if err := p.CopyChecked("files/"+f.Path, &out, f.Size, f.SHA256); err != nil || !bytes.Equal(out.Bytes(), src) {
			t.Errorf("%s: packed bytes differ: %v", f.Path, err)
		}
	}
	if strings.Join(paths, ",") != "README.md,data/settings.json,hello.exe" || m.License != "Fixture license.\n\nNothing here is real: Generic Company Inc. is fictional.\n" || m.Maker == "" {
		t.Fatalf("manifest %v %q", paths, m.License)
	}
	if bytes.Contains(b, []byte(dir)) || bytes.Contains(b, []byte(filepath.ToSlash(dir))) {
		t.Fatal("the manifest names a folder of the build machine")
	}
	// the setup.exe carries hello.exe's icon (no icon in setup.json) and its own version info
	exe, _ := os.ReadFile(r.Path)
	res, err := winres.Resources(exe[:p.Stub])
	if err != nil {
		t.Fatal(err)
	}
	ico, _ := p.ReadAll("app.ico", 1<<20)
	if got, err := winres.ICOFromResources(res); err != nil || !bytes.Equal(got, ico) {
		t.Fatalf("setup.exe icon differs from app.ico: %v", err)
	}
	var version []byte
	for _, x := range res {
		if x.Type == winres.TypeVersion {
			version = x.Data
		}
	}
	v, strs, err := winres.ReadVersionInfo(version)
	if err != nil || v != [4]uint16{1, 0, 0, 0} || strs["FileDescription"] != "Wizard Fixture Setup" {
		t.Fatalf("version info %v %v %v", v, strs, err)
	}
}

// TestBuildIsReproducible pins byte-identical output: two builds of one folder, and a third from a
// copy of it elsewhere, have the same SHA-256.
func TestBuildIsReproducible(t *testing.T) {
	dir := testexe.Fixture(t)
	a := build(t, dir)
	b := build(t, dir)
	moved := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.CopyFS(moved, os.DirFS(dir)); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(filepath.Join(moved, "dist"))
	c := build(t, moved)
	if a.SHA256 != b.SHA256 || a.SHA256 != c.SHA256 {
		t.Fatalf("builds differ: %s %s %s", a.SHA256, b.SHA256, c.SHA256)
	}
}

// TestBuildRefusesABadSpec pins that a bad version comes back as a *SpecError naming the field.
func TestBuildRefusesABadSpec(t *testing.T) {
	dir := testexe.Fixture(t)
	s, _ := spec.Load(filepath.Join(dir, "setup.json"))
	s.Version = "one"
	stub, _ := os.ReadFile(testexe.Maker(t))
	_, err := Build(s, stub, nil)
	var se *SpecError
	if !errors.As(err, &se) || !strings.Contains(err.Error(), "version") {
		t.Fatalf("%v", err)
	}
}

// run starts the test maker in dir with args and returns its combined output and exit code. With
// its standard output a pipe, the maker prints there instead of showing a message box.
func run(t *testing.T, dir string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(testexe.Maker(t), args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return string(out), ee.ExitCode()
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(out), 0
}

// TestCommandLine pins the command line's exit codes and messages: build with -o works, flags after
// setup.json and a missing file are usage errors (2), a bad setup.json lists every problem with 2,
// -version prints the name and version, and an unknown flag is 2.
func TestCommandLine(t *testing.T) {
	dir := testexe.Fixture(t)
	out, code := run(t, dir, "build", "-o", "made.exe", "setup.json")
	if code != 0 || !strings.Contains(out, "Wrote") {
		t.Fatalf("build: %d %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "made.exe")); err != nil {
		t.Fatal(err)
	}
	if out, code := run(t, dir, "build", "setup.json", "-o", "x.exe"); code != 2 || !strings.Contains(out, "flags go before") {
		t.Fatalf("flags after the file: %d %s", code, out)
	}
	if _, code := run(t, dir, "build"); code != 2 {
		t.Fatalf("no file: %d", code)
	}
	os.WriteFile(filepath.Join(dir, "bad.json"), []byte(`{"id": "x y", "files": []}`), 0o644)
	if out, code := run(t, dir, "build", "bad.json"); code != 2 || !strings.Contains(out, "id:") || !strings.Contains(out, "nothing to pack") {
		t.Fatalf("bad spec: %d %s", code, out)
	}
	if out, code := run(t, dir, "-version"); code != 0 || !strings.Contains(out, "Generic Company Wizard Maker 0.1.0") {
		t.Fatalf("-version: %d %s", code, out)
	}
	if _, code := run(t, dir, "-bogus"); code != 2 {
		t.Fatalf("-bogus: %d", code)
	}
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=95fa269a8815f46e99f0e2dd82b1dd9d28be2c7786418937ce6e8000ed06e4f8 ====
