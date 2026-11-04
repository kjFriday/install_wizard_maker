// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/spec/spec_test.go
// About: Tests for package spec: setup.json, its checks, placeholders, versions and manifests.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Tests for package spec: setup.json, its checks, placeholders, versions and manifests.
//
// They cover loading and saving, every Check rule, installed-path and purge-path rules,
// placeholders, version order and the manifest's tamper checks. Most start from valid, a passing
// spec in a temp folder, and break one thing.

package spec

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeTree writes files (slash-separated name -> content) under dir, making folders as needed.
func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// valid is a setup.json that passes Check, in a temp folder with its files.
//
// The tree has files the exclude patterns drop (__pycache__, test_*), a license with a byte-order
// mark, a single-file rule into "docs", and args with a placeholder.
func valid(t *testing.T) *Spec {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"app/hello.exe":         "MZ",
		"app/README.md":         "read me",
		"app/data/a.json":       "{}",
		"app/__pycache__/x.pyc": "cache",
		"app/test_hello.py":     "test",
		"LICENSE":               "\xef\xbb\xbfLicense text",
		"icon/app.ico":          "ico",
		"extra/notes.txt":       "notes",
		"setup.json":            "",
	})
	s := New(dir)
	s.ID, s.Name, s.Version, s.Publisher, s.Group = "GenericCompany.Hello", "Generic Company Hello", "1.2.0", "Generic Company Inc. (fictional)", "Generic Company Inc"
	s.Icon, s.License = "icon/app.ico", "LICENSE"
	s.Files = []FileRule{{From: "app"}, {From: "extra/notes.txt", To: "docs"}}
	s.Exclude = []string{"__pycache__", "test_*"}
	s.Run = Run{Program: "hello.exe", Args: []string{"--db", "{localappdata}\\Generic Company Inc\\generic_company.db"}}
	s.Purge = []string{`{localappdata}\Generic Company Inc\GenericCompany.Hello`}
	return s
}

// TestLoadDefaultsAndUnknownFields pins that a sparse file with a BOM keeps New's defaults and that
// a misspelled field ("shortcut") is an error.
func TestLoadDefaultsAndUnknownFields(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "setup.json")
	writeTree(t, dir, map[string]string{"setup.json": "\xef\xbb\xbf{\"id\": \"A.B\", \"name\": \"AB\"}"})
	s, err := Load(file)
	if err != nil {
		t.Fatal(err)
	}
	if !s.AskFolder || !s.Shortcuts.Ask || !s.Shortcuts.StartMenu || s.Shortcuts.Desktop || s.Dir() != dir {
		t.Fatalf("defaults not kept: %+v", s)
	}
	writeTree(t, dir, map[string]string{"setup.json": `{"id": "A.B", "shortcut": {}}`})
	if _, err := Load(file); err == nil || !strings.Contains(err.Error(), `unknown field "shortcut"`) {
		t.Fatalf("typo not caught: %v", err)
	}
}

// TestSaveRoundTrip pins that Save then Load gives back an equal spec, its folder included.
func TestSaveRoundTrip(t *testing.T) {
	s := valid(t)
	file := filepath.Join(s.Dir(), "setup.json")
	if err := s.Save(file); err != nil {
		t.Fatal(err)
	}
	back, err := Load(file)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, s) {
		t.Fatalf("round trip:\n%+v\n%+v", back, s)
	}
}

// TestValidSpec pins the passing spec's sorted sources (excluded files gone), the BOM-less license
// and the default output name.
func TestValidSpec(t *testing.T) {
	s := valid(t)
	if err := s.Check(); err != nil {
		t.Fatal(err)
	}
	src, err := s.Sources()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range src {
		got = append(got, f.Path)
	}
	want := []string{"README.md", "data/a.json", "docs/notes.txt", "hello.exe"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sources %v, want %v", got, want)
	}
	if text, _ := s.LicenseText(); text != "License text" {
		t.Fatalf("license %q", text)
	}
	out, _ := s.OutputPath()
	if out != filepath.Join(s.Dir(), "Generic Company Hello-1.2.0-setup.exe") {
		t.Fatalf("output %s", out)
	}
}

// TestCheckReportsProblems breaks the valid spec one way per case and checks that Check names the
// problem; the cases cover each rule in check.go and sources.go.
func TestCheckReportsProblems(t *testing.T) {
	cases := []struct {
		name string
		edit func(s *Spec)
		want string
	}{
		{"bad id", func(s *Spec) { s.ID = "Generic Company Hello" }, "id:"},
		{"bad name", func(s *Spec) { s.Name = "Generic Company: Hello" }, "name:"},
		{"device name", func(s *Spec) { s.Name = "NUL" }, "reserved device name"},
		{"trailing dot", func(s *Spec) { s.Group = "GenericCompany." }, "group:"},
		{"bad version", func(s *Spec) { s.Version = "1.2-beta" }, "version:"},
		{"big version", func(s *Spec) { s.Version = "1.70000" }, "larger than 65535"},
		{"no publisher", func(s *Spec) { s.Publisher = " " }, "publisher:"},
		{"missing icon", func(s *Spec) { s.Icon = "nope.ico" }, "icon:"},
		{"missing license", func(s *Spec) { s.License = "NOPE" }, "license:"},
		{"no program", func(s *Spec) { s.Run.Program = "" }, "run.program: missing"},
		{"program not packed", func(s *Spec) { s.Run.Program = "other.exe" }, "not one of the packed files"},
		{"python without version", func(s *Spec) { s.Run.Program = Python }, "needs requires.python"},
		{"bad python", func(s *Spec) { s.Run.Program, s.Requires.Python = Python, "2.7" }, "requires.python"},
		{"unknown placeholder", func(s *Spec) { s.Run.Args = []string{"{home}"} }, "unknown placeholder {home}"},
		{"stray brace", func(s *Spec) { s.Run.Args = []string{"{app"} }, "stray"},
		{"bad aumid", func(s *Spec) { s.Run.AppUserModelID = "has space" }, "appUserModelId"},
		{"shallow purge", func(s *Spec) { s.Purge = []string{`{localappdata}\Generic Company Inc`} }, "two folders deep"},
		{"purge elsewhere", func(s *Spec) { s.Purge = []string{`C:\Windows\System32`} }, "purge:"},
		{"purge escapes", func(s *Spec) { s.Purge = []string{`{appdata}\..\..\x`} }, "purge:"},
		{"bad pattern", func(s *Spec) { s.Exclude = []string{"[x"} }, "bad pattern"},
		{"no files", func(s *Spec) { s.Files = nil }, "nothing to pack"},
		{"missing file", func(s *Spec) { s.Files = append(s.Files, FileRule{From: "gone"}) }, "gone"},
		{"to escapes", func(s *Spec) { s.Files[1].To = "../up" }, "inside the install folder"},
		{"duplicate", func(s *Spec) {
			s.Files = append(s.Files, FileRule{From: "extra/notes.txt", To: "DOCS"})
		}, "comes from both"},
		{"reserved name", func(s *Spec) { s.Files = append(s.Files, FileRule{From: "icon/app.ico"}) }, "written by setup"},
		{"output not exe", func(s *Spec) { s.Output = "{name}.zip" }, "should end in .exe"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := valid(t)
			c.edit(s)
			err := s.Check()
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want %q, got %v", c.want, err)
			}
		})
	}
}

// TestCheckInstalledPath pins which installed paths pass: setup's own names only at the top,
// escapes, drives, device names, bad endings and the 160-character cap (81 "a/" parts plus "b").
func TestCheckInstalledPath(t *testing.T) {
	for p, ok := range map[string]bool{
		"a.txt": true, "sub/dir/a.txt": true, "install.json/x": true, "www/app.ico": true,
		"": false, "/a": false, "a\\b": false, "../a": false, "a/../../b": false, "C:/a": false,
		"a:b": false, "con.txt": false, "sub/LPT1": false, "a./b": false, "a /b": false,
		"install.json": false, "Uninstall.exe": false, "APP.ICO": false, ".setup/new/a": false,
		strings.Repeat("a/", 81) + "b": false,
	} {
		if err := CheckInstalledPath(p); (err == nil) != ok {
			t.Errorf("%q: %v", p, err)
		}
	}
}

// TestCompareVersions pins numeric (not text) order and that missing parts count as 0.
func TestCompareVersions(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{{"1.10", "1.9", 1}, {"1.2", "1.2.0", 0}, {"0.2.0", "0.10.0", -1}, {"2", "1.99.99", 1}, {"1.0.0.1", "1", 1}} {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("%s vs %s = %d", c.a, c.b, got)
		}
	}
}

// TestExpand pins expansion of known placeholders and errors for an unknown one and for unpaired
// or doubled braces.
func TestExpand(t *testing.T) {
	v := ArgVars(`C:\App`, "Hello", "1.0", `C:\L`, `C:\R`)
	got, err := Expand(`{app}\main.py --v {version}`, v)
	if err != nil || got != `C:\App\main.py --v 1.0` {
		t.Fatalf("%q %v", got, err)
	}
	for _, bad := range []string{"{home}", "{app", "app}", "{{app}}"} {
		if _, err := Expand(bad, v); err == nil {
			t.Errorf("%q expanded", bad)
		}
	}
}

// TestPurgePath pins both roots and either slash, and refuses a bare or shallow root, a ".." escape,
// an unknown root and an absolute path.
func TestPurgePath(t *testing.T) {
	got, err := PurgePath(`{localappdata}\Generic Company Inc\GenericCompany.Hello`, `C:\U\Local`, `C:\U\Roaming`)
	if err != nil || got != `C:\U\Local\Generic Company Inc\GenericCompany.Hello` {
		t.Fatalf("%q %v", got, err)
	}
	got, err = PurgePath(`{appdata}/Generic Company Inc/Hello`, `C:\U\Local`, `C:\U\Roaming`)
	if err != nil || got != `C:\U\Roaming\Generic Company Inc\Hello` {
		t.Fatalf("%q %v", got, err)
	}
	for _, bad := range []string{`{localappdata}`, `{localappdata}\Generic Company Inc`, `{appdata}\..\x\y`, `{temp}\a\b`, `C:\a\b\c`} {
		if _, err := PurgePath(bad, `C:\U\Local`, `C:\U\Roaming`); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// TestManifestCheck pins the manifest's JSON round trip and that ParseManifest refuses each kind of
// tampering a setup.exe could carry; each case edits a copy with its own Files slice.
func TestManifestCheck(t *testing.T) {
	s := valid(t)
	files := []File{{Path: "hello.exe", Size: 2, SHA256: strings.Repeat("ab", 32)},
		{Path: "data/a.json", Size: 2, SHA256: strings.Repeat("cd", 32)}}
	m := s.Manifest(files, "text", "Generic Company Wizard Maker 0.1.0")
	back, err := ParseManifest(m.JSON())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, m) || back.Size() != 4 {
		t.Fatalf("round trip %+v", back)
	}
	for name, edit := range map[string]func(m *Manifest){
		"escape":   func(m *Manifest) { m.Files[1].Path = "../x" },
		"twice":    func(m *Manifest) { m.Files[1].Path = "HELLO.EXE" },
		"hash":     func(m *Manifest) { m.Files[0].SHA256 = "zz" },
		"program":  func(m *Manifest) { m.Run.Program = "gone.exe" },
		"newer":    func(m *Manifest) { m.Format = Format + 1 },
		"reserved": func(m *Manifest) { m.Files[1].Path = "uninstall.exe" },
		"purge":    func(m *Manifest) { m.Purge = []string{`C:\Users`} },
	} {
		c := *m
		c.Files = append([]File(nil), m.Files...)
		edit(&c)
		if _, err := ParseManifest(c.JSON()); err == nil {
			t.Errorf("%s: tampered manifest accepted", name)
		}
	}
	if _, err := ParseManifest([]byte(`{"format":1,"extra":true}`)); err == nil {
		t.Error("unknown field accepted")
	}
}

// SetDir moves the folder the spec's relative paths start from (rule 11).
func TestSetDir(t *testing.T) {
	s := valid(t)
	moved := t.TempDir()
	writeTree(t, moved, map[string]string{"app/hello.exe": "MZ", "extra/notes.txt": "notes"})
	s.SetDir(moved)
	if s.Dir() != moved || s.Abs("app") != filepath.Join(moved, "app") || s.Abs(`C:\elsewhere`) != `C:\elsewhere` {
		t.Fatalf("dir %s, app at %s", s.Dir(), s.Abs("app"))
	}
	src, err := s.Sources()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range src {
		got = append(got, f.Path+" "+f.Abs)
	}
	want := []string{"docs/notes.txt " + filepath.Join(moved, "extra", "notes.txt"), "hello.exe " + filepath.Join(moved, "app", "hello.exe")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sources %v, want %v", got, want)
	}
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=5ee9e39e6392a9a4b62fccd9c5f677b3120b24c49c69a518f21d99bbf04c1d69 ====
