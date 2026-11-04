// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/spec/spec.go
// About: Package spec reads and checks setup.json, the description of one install wizard, and...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Package spec reads and checks setup.json, the description of one install wizard, and turns it into
// the manifest a setup.exe carries.
//
// Paths in setup.json are relative to the file itself. A file rule copies a file, or a folder's
// contents, into the folder "to" of the install folder (default: the install folder itself).
package spec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Spec is setup.json.
type Spec struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Version   string     `json:"version"`
	Publisher string     `json:"publisher"`
	Group     string     `json:"group,omitempty"`
	Icon      string     `json:"icon,omitempty"`
	License   string     `json:"license,omitempty"`
	Files     []FileRule `json:"files"`
	Exclude   []string   `json:"exclude,omitempty"`
	Run       Run        `json:"run"`
	Requires  Requires   `json:"requires,omitzero"`
	AskFolder bool       `json:"askFolder"`
	Shortcuts Shortcuts  `json:"shortcuts"`
	Purge     []string   `json:"purge,omitempty"`
	Output    string     `json:"output,omitempty"`

	dir string // the folder setup.json is in
}

// FileRule packs a file, or a folder's contents, into folder To of the install folder.
type FileRule struct {
	From string `json:"from"`
	To   string `json:"to,omitempty"`
}

// Run is what the shortcuts start: a packed file, or {python} with Requires.Python.
type Run struct {
	Program        string   `json:"program"`
	Args           []string `json:"args,omitempty"`
	AppUserModelID string   `json:"appUserModelId,omitempty"`
}

// Requires names software setup must find on the computer.
type Requires struct {
	Python string `json:"python,omitempty"` // lowest CPython version, e.g. "3.10"
}

// Shortcuts: whether setup asks, and which shortcuts are ticked.
type Shortcuts struct {
	Ask       bool `json:"ask"`
	StartMenu bool `json:"startMenu"`
	Desktop   bool `json:"desktop"`
}

// Python is the run.program placeholder for the newest registered CPython's pythonw.exe.
const Python = "{python}"

// New is an empty spec with the defaults: ask for the folder, ask about shortcuts, Start menu ticked.
func New(dir string) *Spec {
	return &Spec{AskFolder: true, Shortcuts: Shortcuts{Ask: true, StartMenu: true}, dir: dir}
}

// Load reads setup.json. Unknown fields are errors, so a typo cannot pass silently.
func Load(file string) (*Spec, error) {
	abs, err := filepath.Abs(file)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	s := New(filepath.Dir(abs))
	dec := json.NewDecoder(bytes.NewReader(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))))
	dec.DisallowUnknownFields()
	if err := dec.Decode(s); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(abs), err)
	}
	return s, nil
}

// Save writes the spec as setup.json (indented, fields in a fixed order) and makes file its folder.
func (s *Spec) Save(file string) error {
	abs, err := filepath.Abs(file)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := abs + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, abs); err != nil {
		os.Remove(tmp)
		return err
	}
	s.dir = filepath.Dir(abs)
	return nil
}

// Dir is the folder relative paths start from.
func (s *Spec) Dir() string { return s.dir }

// SetDir changes the folder relative paths start from.
func (s *Spec) SetDir(dir string) { s.dir = dir }

// Abs resolves a path from setup.json.
func (s *Spec) Abs(p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(s.dir, filepath.FromSlash(p))
}

// LicenseText is the license file as text (UTF-8, at most 1 MiB, byte-order mark removed).
func (s *Spec) LicenseText() (string, error) {
	if s.License == "" {
		return "", nil
	}
	b, err := os.ReadFile(s.Abs(s.License))
	if err != nil {
		return "", err
	}
	if len(b) > 1<<20 {
		return "", fmt.Errorf("%s is larger than 1 MiB", s.License)
	}
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	if !utf8.Valid(b) {
		return "", fmt.Errorf("%s is not UTF-8 text", s.License)
	}
	return string(b), nil
}

// OutputPath is where the setup.exe goes: output (default "{name}-{version}-setup.exe") beside setup.json.
func (s *Spec) OutputPath() (string, error) {
	out := s.Output
	if out == "" {
		out = "{name}-{version}-setup.exe"
	}
	name, err := Expand(out, Vars{"name": s.Name, "version": s.Version, "id": s.ID})
	if err != nil {
		return "", fmt.Errorf("output: %w", err)
	}
	if !strings.EqualFold(filepath.Ext(name), ".exe") {
		return "", fmt.Errorf("output: %s should end in .exe", name)
	}
	return s.Abs(name), nil
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=3a1b492b4662f05bdf2c330026b2f4931ca43f3f9c22bb0c370ef3f89837f1b0 ====
