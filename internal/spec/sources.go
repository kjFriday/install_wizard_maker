// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/spec/sources.go
// About: Sources walks a spec's file rules: every file to pack, and where it is installed.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Sources walks a spec's file rules: every file to pack, and where it is installed.
//
// Check calls Sources to vet the rules, the maker's build packs what it returns, and the maker's
// form lists it. A file rule names one file (installed as To/<its name>) or a folder (its contents
// installed under To, keeping their relative paths). Exclude patterns apply inside folder rules
// only; a rule that names a single file is always packed.

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
	Size int64  // bytes, when the walk saw it
}

// Sources walks the file rules: every file to pack, sorted by installed path.
//
// It reads the disk but writes nothing. The first problem stops the walk: a rule without "from", a
// bad "to", a missing source, an invalid installed path, or two sources landing on one installed
// path (compared ignoring case, as Windows would overwrite one with the other). No files at all is
// an error too. Links are followed to their files; the sort is by byte order of Path.
//
// Steps:
//  1. For each rule, check "to" and stat "from".
//  2. A file goes straight in under "to".
//  3. A folder is walked; excluded names are skipped (a whole folder at once), and every regular
//     file goes in under "to" with its path relative to the folder.
//  4. Sort the result by installed path.
func (s *Spec) Sources() ([]Source, error) {
	var out []Source
	seen := map[string]string{} // lower-cased installed path -> the source file that claimed it
	// add records one file after checking its installed path and that no other file claimed it
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
			// a broken link fails the walk; anything else that is not a regular file is skipped
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

// excluded reports whether rel (slash-separated, relative to the rule's folder) is left out.
//
// A pattern without "/" matches any file or folder name, one with "/" the whole relative path.
// Patterns use path.Match syntax (*, ?, [a-z]) and ignore case; "*" never crosses a "/". A bad
// pattern matches nothing here, and Check reports it.
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

// ==== stamp v1 | author=kjFriday | hmac-sha256=063c9214f4c496a9781fd95330832e388f004c2c1626c9fbfc2f18d0cbc248d1 ====
