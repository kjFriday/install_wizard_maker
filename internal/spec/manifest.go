// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/spec/manifest.go
// About: The manifest: the checked, hashed form of setup.json that a setup.exe carries.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// The manifest: the checked, hashed form of setup.json that a setup.exe carries.
//
// The maker's build hashes the packed files and calls Spec.Manifest, then stores Manifest.JSON in
// the setup's zip as setup.json. The wizard reads it back with ParseManifest, which re-runs every
// check that keeps an install safe, because anyone can edit a setup.exe after it was made. Package
// install then trusts the manifest's paths, sizes and hashes when it unpacks and verifies files.
//
// Types: Manifest (the record) and File (one packed file with its size and SHA-256).

package spec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Format is the manifest format this maker writes and reads.
//
// A wizard refuses a manifest with a higher number (Manifest.Check), so a future format change
// that older wizards cannot follow bumps it.
const Format = 1

// Manifest is what a setup.exe carries (as setup.json in its zip): the spec with every file resolved
// and hashed, the license text itself, and no path from the computer it was built on.
//
// The fields mean what they mean in Spec. License holds the text, not a file name; Files is sorted
// by path as Sources sorts them; Maker names the maker and version that built the setup.
type Manifest struct {
	Format    int       `json:"format"`
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Version   string    `json:"version"`
	Publisher string    `json:"publisher"`
	Group     string    `json:"group,omitempty"`
	Run       Run       `json:"run"`
	Requires  Requires  `json:"requires,omitzero"`
	AskFolder bool      `json:"askFolder"`
	Shortcuts Shortcuts `json:"shortcuts"`
	Purge     []string  `json:"purge,omitempty"`
	License   string    `json:"license,omitempty"`
	Files     []File    `json:"files"`
	Maker     string    `json:"maker"`
}

// File is one packed file.
//
// Size is in bytes. SHA256 is the file's SHA-256 digest as 64 lowercase hex digits; setup checks
// each unpacked file against it, and the uninstaller removes only files that still match.
type File struct {
	Path   string `json:"path"` // slash-separated, relative to the install folder
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Manifest is the spec's manifest for these (already hashed) files.
//
// license is the license text (from LicenseText) and maker the maker's name and version. It does
// not check anything itself; the build runs Check on the result. run.program is normalized to slashes and to
// the packed file's own spelling, so the case-insensitive match in Manifest.Check and the
// shortcut's target agree with the file on disk.
func (s *Spec) Manifest(files []File, license, maker string) *Manifest {
	run := s.Run
	if run.Program != Python {
		run.Program = strings.Trim(filepath.ToSlash(run.Program), "/")
		for _, f := range files {
			if strings.EqualFold(f.Path, run.Program) {
				run.Program = f.Path // the packed file's own spelling
			}
		}
	}
	return &Manifest{Format: Format, ID: s.ID, Name: s.Name, Version: s.Version, Publisher: s.Publisher,
		Group: s.Group, Run: run, Requires: s.Requires, AskFolder: s.AskFolder, Shortcuts: s.Shortcuts,
		Purge: s.Purge, License: license, Files: files, Maker: maker}
}

// JSON is the manifest as stored in a setup.exe.
//
// It is indented and ends in a newline. The marshal error is dropped: every field is a plain
// string, number, bool or slice of them, which cannot fail to encode.
func (m *Manifest) JSON() []byte {
	b, _ := json.MarshalIndent(m, "", "  ")
	return append(b, '\n')
}

// ParseManifest reads and checks a setup.exe's manifest.
//
// Unknown fields are errors, as in Load. The wizard and the maker's tests call it; any error means
// the setup file cannot be trusted.
func ParseManifest(b []byte) (*Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("setup.json in the setup file: %w", err)
	}
	if err := m.Check(); err != nil {
		return nil, err
	}
	return &m, nil
}

// hexPattern is a SHA-256 digest as the maker writes it: 32 bytes, 64 lowercase hex digits.
var hexPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Check re-checks everything a setup.exe relies on, since the file may have been changed after it was made.
//
// It repeats the spec's identity, run and purge checks, and adds what only a manifest has: every
// file path valid and listed once (ignoring case, as Windows does), a sane size and digest, at
// least one file, and run.program among them. A format of 0 (the field left out) is accepted.
func (m *Manifest) Check() error {
	if m.Format > Format {
		return fmt.Errorf("this setup file needs a newer wizard (format %d)", m.Format)
	}
	errs := checkIdentity(m.ID, m.Name, m.Version, m.Publisher, m.Group)
	errs = append(errs, checkRun(m.Run, m.Requires)...)
	for _, p := range m.Purge {
		if err := checkPurge(p); err != nil {
			errs = append(errs, err)
		}
	}
	seen := map[string]bool{}
	for _, f := range m.Files {
		if err := CheckInstalledPath(f.Path); err != nil {
			errs = append(errs, fmt.Errorf("files: %s: %w", f.Path, err))
		}
		if key := strings.ToLower(f.Path); seen[key] {
			errs = append(errs, fmt.Errorf("files: %s is listed twice", f.Path))
		} else {
			seen[key] = true
		}
		if f.Size < 0 || !hexPattern.MatchString(f.SHA256) {
			errs = append(errs, fmt.Errorf("files: %s has a bad size or SHA-256", f.Path))
		}
	}
	if len(m.Files) == 0 {
		errs = append(errs, errors.New("files: nothing to install"))
	}
	if m.Run.Program != Python && !seen[strings.ToLower(m.Run.Program)] {
		errs = append(errs, fmt.Errorf("run.program: %s is not one of the packed files", m.Run.Program))
	}
	return errors.Join(errs...)
}

// Size is the total size of the packed files, in bytes (as installed, not as compressed).
func (m *Manifest) Size() int64 {
	var n int64
	for _, f := range m.Files {
		n += f.Size
	}
	return n
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=e0ee2a5456cd7639475f1e6dd95c00e547074a59a1760f6fcff7dbb0d96585c1 ====
