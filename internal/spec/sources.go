// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/spec/sources.go
// About: Sources walks a spec's file rules: every file to pack, and where it is installed.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Sources walks a spec's file rules: every file to pack, and where it is installed.

package spec

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Source is one file to pack.
type Source struct {
	Path string // installed path: slash-separated, relative to the install folder
	Abs  string // the file on this computer
	Size int64
}

// Sources walks the file rules: every file to pack, sorted by installed path.
func (s *Spec) Sources() ([]Source, error) {
	var out []Source
	seen := map[string]string{}
	add := func(dest, abs string, size int64) error {
		if err := CheckInstalledPath(dest); err != nil {
			return fmt.Errorf("files: %s: %w", dest, err)
		}
		key := strings.ToLower(dest)
		if prev, ok := seen[key]; ok {
			return fmt.Errorf("files: %s comes from both %s and %s", dest, prev, abs)
		}
		seen[key] = abs
		out = append(out, Source{Path: dest, Abs: abs, Size: size})
		return nil
	}
	for _, r := range s.Files {
		if r.From == "" {
			return nil, errors.New("files: a rule has no \"from\"")
		}
		to := strings.Trim(filepath.ToSlash(r.To), "/")
		if to != "" {
			if err := CheckInstalledPath(to); err != nil {
				return nil, fmt.Errorf("files: to %q: %w", r.To, err)
			}
		}
		from := s.Abs(r.From)
		info, err := os.Stat(from)
		if err != nil {
			return nil, fmt.Errorf("files: %s: %w", r.From, err)
		}
		if !info.IsDir() {
			if err := add(path.Join(to, filepath.Base(from)), from, info.Size()); err != nil {
				return nil, err
			}
			continue
		}
		err = filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if p == from {
				return nil
			}
			rel, _ := filepath.Rel(from, p)
			rel = filepath.ToSlash(rel)
			if s.excluded(rel) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			info, err := os.Stat(p) // follows a link to a file
			if err != nil || !info.Mode().IsRegular() {
				return err
			}
			return add(path.Join(to, rel), p, info.Size())
		})
		if err != nil {
			return nil, err
		}
	}
	if len(out) == 0 {
		return nil, errors.New("files: nothing to pack")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// excluded: a pattern without "/" matches any file or folder name, one with "/" the whole relative path.
func (s *Spec) excluded(rel string) bool {
	for _, p := range s.Exclude {
		target := path.Base(rel)
		if strings.Contains(p, "/") {
			target = rel
		}
		if ok, _ := path.Match(strings.ToLower(p), strings.ToLower(target)); ok {
			return true
		}
	}
	return false
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=40f25026aa6622e1cfbc51f88e04c48a35a6108c6d3c0062cbb8b87041943533 ====
