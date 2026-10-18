// ==== ACME Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/install/plan_test.go
// About: Tests for package install.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

package install

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"acme/wizard/internal/spec"
)

var today = time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)

func hash(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// memSource is a payload in memory, checked the way payload.File checks.
type memSource map[string][]byte

func (m memSource) CopyChecked(name string, w io.Writer, size int64, want string) error {
	b, ok := m[strings.TrimPrefix(name, "files/")]
	if !ok {
		return fmt.Errorf("%s missing", name)
	}
	if int64(len(b)) != size || hash(b) != want {
		return fmt.Errorf("%s damaged", name)
	}
	_, err := w.Write(b)
	return err
}

func manifest(version string, files memSource) *spec.Manifest {
	m := &spec.Manifest{Format: 1, ID: "ACME.Hello", Name: "ACME Hello", Version: version, Publisher: "ACME Inc. (fictional)",
		Group: "ACME Inc", Run: spec.Run{Program: "hello.exe", Args: []string{"--db", `{localappdata}\ACME Inc\acme.db`}},
		AskFolder: true, Shortcuts: spec.Shortcuts{Ask: true, StartMenu: true},
		Purge: []string{`{localappdata}\ACME Inc\ACME.Hello`}, Maker: "ACME Wizard Maker test"}
	var names []string
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		m.Files = append(m.Files, spec.File{Path: n, Size: int64(len(files[n])), SHA256: hash(files[n])})
	}
	return m
}

// memRegistry is the Uninstall key in memory.
type memRegistry struct {
	keys map[string][]Value
	fail bool
}

func (r *memRegistry) Read(id string) ([]Value, bool, error) {
	v, ok := r.keys[id]
	return append([]Value(nil), v...), ok, nil
}

func (r *memRegistry) Write(id string, vals []Value) error {
	if r.fail {
		return errors.New("registry is read-only")
	}
	r.keys[id] = append([]Value(nil), vals...)
	return nil
}

func (r *memRegistry) Delete(id string) error {
	delete(r.keys, id)
	return nil
}

func (r *memRegistry) dump() string {
	b, _ := json.Marshal(r.keys)
	return string(b)
}

type sandbox struct {
	pl  Places
	reg *memRegistry
}

func newSandbox(t *testing.T) *sandbox {
	return &sandbox{pl: SandboxPlaces(t.TempDir()), reg: &memRegistry{keys: map[string][]Value{}}}
}

func (s *sandbox) dir() string { return s.pl.Dir("ACME Inc", "ACME Hello") }

var v1 = memSource{"hello.exe": []byte("MZ hello 1"), "README.md": []byte("read me"), "data/old.json": []byte("{}"),
	"data/settings.json": []byte(`{"v":1}`)}

func TestPlanNewInstall(t *testing.T) {
	s := newSandbox(t)
	p, err := MakePlan(manifest("1.0.0", v1), s.pl, Choices{Dir: s.dir(), StartMenu: true, Desktop: true}, nil, today, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{p.Links[0].Path, p.Links[1].Path}; !reflect.DeepEqual(got, []string{
		filepath.Join(s.pl.Root, "Start Menu", "ACME Inc", "ACME Hello.lnk"), filepath.Join(s.pl.Root, "Desktop", "ACME Hello.lnk")}) {
		t.Fatalf("links %v", got)
	}
	l := p.Links[0]
	if l.Target != filepath.Join(s.dir(), "hello.exe") || l.WorkDir != s.dir() || l.Icon != filepath.Join(s.dir(), "app.ico") ||
		l.Args != `--db "`+filepath.Join(s.pl.LocalAppData, "ACME Inc", "acme.db")+`"` {
		t.Fatalf("shortcut %+v", l)
	}
	want := map[string]string{
		"DisplayName":          "ACME Hello 1.0.0",
		"InstallLocation":      s.dir(),
		"UninstallString":      `"` + filepath.Join(s.dir(), "uninstall.exe") + `" -root "` + s.pl.Root + `"`,
		"QuietUninstallString": `"` + filepath.Join(s.dir(), "uninstall.exe") + `" -root "` + s.pl.Root + `" -quiet`,
		"InstallDate":          "20261017",
		"Publisher":            "ACME Inc. (fictional)",
	}
	for name, text := range want {
		if v, _ := Lookup(p.Registry, name); v.Text != text || v.Type != RegSZ {
			t.Errorf("%s = %q, want %q", name, v.Text, text)
		}
	}
	if v, _ := Lookup(p.Registry, "EstimatedSize"); v.Type != RegDWORD || v.Number != 5 {
		t.Errorf("EstimatedSize %+v", v)
	}
	if !reflect.DeepEqual(p.Marker.Files, []string{"README.md", "data/old.json", "data/settings.json", "hello.exe", "app.ico", "uninstall.exe"}) ||
		p.Marker.SHA256["hello.exe"] != hash(v1["hello.exe"]) || len(p.Marker.Shortcuts) != 2 {
		t.Fatalf("marker %+v", p.Marker)
	}
}

func TestPlanRefuses(t *testing.T) {
	s := newSandbox(t)
	c := Choices{Dir: s.dir(), StartMenu: true}
	var r *Refusal
	if _, err := MakePlan(manifest("1.0.0", v1), s.pl, c, &Marker{AppID: "ACME.Hello", Version: "1.10"}, today, 0); !errors.As(err, &r) {
		t.Fatalf("downgrade: %v", err)
	}
	if _, err := MakePlan(manifest("1.0.0", v1), s.pl, c, &Marker{AppID: "ACME.Other", Version: "0.1"}, today, 0); !errors.As(err, &r) {
		t.Fatalf("foreign: %v", err)
	}
	m := manifest("1.0.0", v1)
	m.Run.Program, m.Requires.Python = spec.Python, "3.10"
	if _, err := MakePlan(m, s.pl, c, nil, today, 0); !errors.As(err, &r) || !strings.Contains(err.Error(), "Python 3.10") {
		t.Fatalf("no python: %v", err)
	}
	c.Python = `C:\Python312\pythonw.exe`
	m.Run.Args = []string{`{app}\main.py`}
	p, err := MakePlan(m, s.pl, c, nil, today, 0)
	if err != nil || p.Links[0].Target != c.Python || p.Links[0].Args != `"`+filepath.Join(s.dir(), "main.py")+`"` {
		t.Fatalf("python shortcut: %v %+v", err, p)
	}
}

// The other ACME programs' markers have no hashes: shipped entries are replaced, the rest kept.
func TestPlanOverSiblingMarkers(t *testing.T) {
	s := newSandbox(t)
	for name, files := range map[string][]string{
		"go":     {"acme-hello.exe", "app.ico"},
		"python": {"main.py", "data/x.json", "icon/app.ico"},
		"rust":   {"hello.exe", "www"},
	} {
		old := &Marker{AppID: "ACME.Hello", Version: "0.2.0", Files: files}
		p, err := MakePlan(manifest("1.0.0", v1), s.pl, Choices{Dir: s.dir()}, old, today, 0)
		if err != nil {
			t.Fatal(err)
		}
		var want []string
		for _, f := range files {
			if f != "app.ico" && f != "hello.exe" {
				want = append(want, f)
			}
		}
		sort.Strings(want)
		if len(p.Remove) != 0 || !reflect.DeepEqual(p.Kept, want) {
			t.Errorf("%s: remove %v kept %v", name, p.Remove, p.Kept)
		}
	}
}

func TestCheckFolder(t *testing.T) {
	s := newSandbox(t)
	empty := filepath.Join(s.pl.Programs, "Empty")
	os.MkdirAll(empty, 0o755)
	full := filepath.Join(s.pl.Programs, "Full")
	os.MkdirAll(full, 0o755)
	os.WriteFile(filepath.Join(full, "x.txt"), nil, 0o644)
	ours := filepath.Join(s.pl.Programs, "Ours")
	theirs := filepath.Join(s.pl.Programs, "Theirs")
	for dir, id := range map[string]string{ours: "ACME.Hello", theirs: "ACME.Other"} {
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, MarkerName), (&Marker{AppID: id}).JSON(), 0o644)
	}
	for dir, ok := range map[string]bool{
		filepath.Join(s.pl.Programs, "New"): true, empty: true, ours: true,
		full: false, theirs: false, s.pl.Programs: false, `C:\`: false, "relative": false,
		filepath.Join(s.pl.Programs, strings.Repeat("x", 160)): false,
	} {
		if err := CheckFolder(dir, "ACME.Hello", s.pl); (err == nil) != ok {
			t.Errorf("%s: %v", dir, err)
		}
	}
}

func TestExisting(t *testing.T) {
	s := newSandbox(t)
	if dir, m := Existing(s.reg, s.pl, "ACME.Hello", "ACME Inc", "ACME Hello"); dir != "" || m != nil {
		t.Fatal("found an install in an empty sandbox")
	}
	custom := filepath.Join(s.pl.Root, "Custom", "Hello")
	os.MkdirAll(custom, 0o755)
	os.WriteFile(filepath.Join(custom, MarkerName), (&Marker{AppID: "ACME.Hello", Version: "1.0"}).JSON(), 0o644)
	s.reg.keys["ACME.Hello"] = []Value{Str("InstallLocation", custom)}
	if dir, m := Existing(s.reg, s.pl, "ACME.Hello", "ACME Inc", "ACME Hello"); dir != custom || m.Version != "1.0" {
		t.Fatalf("%s %+v", dir, m)
	}
	if dir, _ := Existing(s.reg, s.pl, "ACME.Other", "ACME Inc", "ACME Other"); dir != "" {
		t.Fatal("another program's folder matched")
	}
}

func TestQuoteArg(t *testing.T) {
	for in, want := range map[string]string{`C:\a b\x.exe`: `"C:\a b\x.exe"`, `C:\dir\`: `"C:\dir\\"`, `C:\`: `"C:\\"`} {
		if got := quoteArg(in); got != want {
			t.Errorf("%s -> %s", in, got)
		}
	}
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=e16227956452865f8dbdd3492a455bf16fe8b7072603566eda853edaa097ef1e ====
