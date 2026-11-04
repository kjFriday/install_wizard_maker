// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/install/fixtures_test.go
// About: Shared install test fixtures: in-memory payload and registry, a sandbox, two versions.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Shared install test fixtures: in-memory payload and registry, a sandbox, two versions.
//
// In full: a payload and a registry in memory, a -root sandbox, and two versions of one program to
// install, upgrade and remove. Every test gets its own sandbox under t.TempDir, so tests never touch
// the real Start menu or registry. Start reading at sandbox and its install method.

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

// hash is the lowercase hex SHA-256 of b, as the marker and manifest store it.
func hash(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// memSource is a payload in memory, checked the way payload.File checks.
// Keys are the installed "/" paths, without the payload's "files/" prefix.
type memSource map[string][]byte

// CopyChecked writes the named file to w, failing as "missing" or "damaged" when it is absent or its
// size or hash differ, like payload.File.CopyChecked.
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
// Its run.args use {localappdata}, so tests see placeholder expansion and quoting; its purge entry is
// the program's own settings folder beside a shared database the purge must leave alone.
// Files are sorted by path, as spec.Sources sorts them.
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
	keys map[string][]Value // app id -> the key's values
	fail bool               // make Write fail, to test the registry phase's rollback
}

// Read returns a copy of id's values, so callers cannot change the stored ones.
func (r *memRegistry) Read(id string) ([]Value, bool, error) {
	v, ok := r.keys[id]
	return append([]Value(nil), v...), ok, nil
}

// Write stores a copy of vals as id's whole key, or fails when fail is set.
func (r *memRegistry) Write(id string, vals []Value) error {
	if r.fail {
		return errors.New("registry is read-only")
	}
	r.keys[id] = append([]Value(nil), vals...)
	return nil
}

// Delete removes id's key; a missing key is not an error, as the interface requires.
func (r *memRegistry) Delete(id string) error {
	delete(r.keys, id)
	return nil
}

// dump is the whole registry as JSON, for snapshots (encoding/json sorts map keys, so equal
// registries dump equal).
func (r *memRegistry) dump() string {
	b, _ := json.Marshal(r.keys)
	return string(b)
}

// sandbox is a -root folder and an in-memory registry for one test.
type sandbox struct {
	pl  Places
	reg *memRegistry
}

// newSandbox makes an empty sandbox in a fresh temp folder, removed when t ends.
func newSandbox(t *testing.T) *sandbox {
	return &sandbox{pl: SandboxPlaces(t.TempDir()), reg: &memRegistry{keys: map[string][]Value{}}}
}

// dir is the test program's default install folder in the sandbox.
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
// c.Dir is always the default folder; the uninstaller size 14 is len("MZ uninstaller"). Any error
// fails the test.
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
// Folders are listed too (with a trailing "/"), so a folder left behind by a rollback shows up.
// WalkDir visits in lexical order, so equal sandboxes give equal strings.
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

// ==== stamp v1 | author=kjFriday | hmac-sha256=24f20d8db61f491f6a9ab85c311da9d74faf783b3e8345bdddb8386c8dcb0b2b ====
