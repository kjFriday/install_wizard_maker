// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/spec/manifest.go
// About: Part of package spec in wizard_maker.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

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
const Format = 1

// Manifest is what a setup.exe carries (as setup.json in its zip): the spec with every file resolved
// and hashed, the license text itself, and no path from the computer it was built on.
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
type File struct {
	Path   string `json:"path"` // slash-separated, relative to the install folder
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Manifest is the spec's manifest for these (already hashed) files.
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
func (m *Manifest) JSON() []byte {
	b, _ := json.MarshalIndent(m, "", "  ")
	return append(b, '\n')
}

// ParseManifest reads and checks a setup.exe's manifest.
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

var hexPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Check re-checks everything a setup.exe relies on, since the file may have been changed after it was made.
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

// Size is the total size of the packed files.
func (m *Manifest) Size() int64 {
	var n int64
	for _, f := range m.Files {
		n += f.Size
	}
	return n
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=fbb4b59c8404109c334e377550ea67d9ec246f442a0ddcd885a12d535ac43127 ====
