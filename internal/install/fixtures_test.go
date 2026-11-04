// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/install/fixtures_test.go
// About: Fixtures the install tests share: a payload and a registry in memory, a -root sandbox...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Fixtures the install tests share: a payload and a registry in memory, a -root sandbox, and two
// versions of one program to install, upgrade and remove.

package install

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"generic-company/wizard/internal/spec"
)

// today is the date every test install is made on.
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

// manifest is the test program at version, packing files.
func manifest(version string, files memSource) *spec.Manifest {
	m := &spec.Manifest{Format: 1, ID: "GenericCompany.Hello", Name: "Generic Company Hello", Version: version, Publisher: "Generic Company Inc. (fictional)",
		Group: "Generic Company Inc", Run: spec.Run{Program: "hello.exe", Args: []string{"--db", `{localappdata}\Generic Company Inc\generic_company.db`}},
		AskFolder: true, Shortcuts: spec.Shortcuts{Ask: true, StartMenu: true},
		Purge: []string{`{localappdata}\Generic Company Inc\GenericCompany.Hello`}, Maker: "Generic Company Wizard Maker test"}
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

// sandbox is a -root folder and an in-memory registry for one test.
type sandbox struct {
	pl  Places
	reg *memRegistry
}

func newSandbox(t *testing.T) *sandbox {
	return &sandbox{pl: SandboxPlaces(t.TempDir()), reg: &memRegistry{keys: map[string][]Value{}}}
}

func (s *sandbox) dir() string { return s.pl.Dir("Generic Company Inc", "Generic Company Hello") }

// v1 is the first version: hello.exe, README.md, data/old.json and data/settings.json.
var v1 = memSource{"hello.exe": []byte("MZ hello 1"), "README.md": []byte("read me"), "data/old.json": []byte("{}"),
	"data/settings.json": []byte(`{"v":1}`)}

// v2 changes hello.exe, keeps README.md, drops both data files and adds data/new.json.
var v2 = memSource{"hello.exe": []byte("MZ hello 2"), "README.md": []byte("read me"), "data/new.json": []byte("[]")}

// fakeLink writes a text file where the .lnk would go.
func fakeLink(s Shortcut) error {
	b, _ := json.Marshal(s)
	return os.WriteFile(s.Path, b, 0o644)
}

// env is what Apply gets in the sandbox: fakes for the shortcuts, the payload and the files setup writes.
func (s *sandbox) env(src memSource) Env {
	return Env{Places: s.pl, Registry: s.reg, Link: fakeLink, Source: src, Icon: []byte("ICON"),
		Uninstaller: []byte("MZ uninstaller"), Retry: time.Millisecond}
}

// install makes and applies a plan for version over whatever the sandbox holds now.
func (s *sandbox) install(t *testing.T, version string, src memSource, c Choices) Result {
	t.Helper()
	old, _ := ReadMarker(s.dir())
	c.Dir = s.dir()
	p, err := MakePlan(manifest(version, src), s.pl, c, old, today, 14)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Apply(p, s.env(src))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// snapshot is every file under the sandbox with its hash, plus the registry.
func (s *sandbox) snapshot(t *testing.T) string {
	t.Helper()
	var lines []string
	filepath.WalkDir(s.pl.Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(s.pl.Root, p)
		if d.IsDir() {
			lines = append(lines, rel+"/")
			return nil
		}
		b, _ := os.ReadFile(p)
		lines = append(lines, rel+" "+hash(b))
		return nil
	})
	return strings.Join(lines, "\n") + "\n" + s.reg.dump()
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=43274b96218fdb8fc80e154657bafb78ace23c2249166afe5d55d906002a4176 ====
