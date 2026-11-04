// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/spec/spec.go
// About: Package spec reads and checks setup.json and turns it into a setup.exe's manifest.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Package spec reads and checks setup.json and turns it into a setup.exe's manifest.
//
// setup.json describes one install wizard: the program's identity, the files to pack, what the
// shortcuts start, and what setup asks. The maker (its form window and its build command) loads,
// edits, checks and saves a Spec; a build turns it into a Manifest. The wizard reads that Manifest
// back out of setup.exe with ParseManifest, and package install expands its placeholders and purge
// entries. The package touches the disk only to read setup.json, the license, the icon and the
// files to pack, and to save setup.json.
//
// Paths in setup.json are relative to the file itself. A file rule copies a file, or a folder's
// contents, into the folder "to" of the install folder (default: the install folder itself).
//
// The flow of a build:
//  1. Load reads setup.json (or New starts an empty one), rejecting unknown fields.
//  2. Check reports every problem at once (check.go).
//  3. Sources walks the file rules into the sorted list of files to pack (sources.go).
//  4. The maker hashes those files, and Manifest turns the spec into the record setup.exe carries
//     (manifest.go); the wizard re-checks it with ParseManifest because the exe may have been edited.
//
// Types: Spec, FileRule, Run, Requires and Shortcuts (setup.json); Manifest and File (manifest.go);
// Source (sources.go); Vars (placeholders.go). Files: spec.go (the setup.json model, load and
// save), check.go, sources.go, manifest.go, placeholders.go ({app} and friends, purge paths) and
// version.go (version order). Start reading at Spec, then Check.
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
//
// The JSON field order here is the order Save writes. ID is the program's id (it names the Apps &
// features key and is recorded in install.json), Name and Group (the publisher's folder, also the
// Start menu folder) name the default install folder, Icon and License are files beside
// setup.json, Exclude name patterns for folder rules (see excluded), Purge the user-data folders
// the uninstaller may remove, and Output the setup.exe's name (see OutputPath). Optional fields are
// left out of the file when empty.
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
//
// From is relative to setup.json (or absolute); To is a slash-separated folder inside the install
// folder, "" for the install folder itself.
type FileRule struct {
	From string `json:"from"`
	To   string `json:"to,omitempty"`
}

// Run is what the shortcuts start: a packed file, or {python} with Requires.Python.
//
// Program is an installed path ("hello.exe", "bin/tool.exe") or the Python placeholder. Args may
// use the placeholders ArgVars lists and are expanded at install time. AppUserModelID, when set,
// goes into each shortcut so the taskbar groups the program's windows under it.
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
//
// dir is the folder relative paths start from ("" until the form saves the spec somewhere).
func New(dir string) *Spec {
	return &Spec{AskFolder: true, Shortcuts: Shortcuts{Ask: true, StartMenu: true}, dir: dir}
}

// Load reads setup.json. Unknown fields are errors, so a typo cannot pass silently.
//
// Decoding starts from New's defaults, so a field the file leaves out keeps its default (askFolder
// and shortcuts.ask stay true). A UTF-8 byte-order mark (EF BB BF, which Notepad may write) is
// dropped first. The spec's folder becomes file's folder. Load does not Check the spec.
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
//
// The file is written to file + ".tmp" first and then renamed over file, so a failed write never
// leaves a half-written setup.json behind. Relative paths in the spec are not rewritten: a spec
// saved into another folder resolves them from there.
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
//
// "" and absolute paths come back as they are; anything else is joined to the spec's folder, with
// "/" accepted as a separator.
func (s *Spec) Abs(p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(s.dir, filepath.FromSlash(p))
}

// LicenseText is the license file as text (UTF-8, at most 1 MiB, byte-order mark removed).
//
// No license gives "" and no error. The text goes into the manifest, and the wizard shows it on
// its license page; the size cap keeps that page and the manifest reasonable.
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
//
// output may use {name}, {version} and {id}; any other placeholder, or a name not ending in .exe
// (any case), is an error. An absolute output is used as it is.
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

// ==== stamp v1 | author=kjFriday | hmac-sha256=e8f3375d8fcd2557f023939bab15ef04cd1c273e294e26c42873e789dcc3ac1b ====
