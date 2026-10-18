// ==== ACME Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/maker/form.go
// About: Part of package maker in install_wizard_maker.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

package maker

import (
	"path/filepath"
	"strings"

	"acme/wizard/internal/spec"
)

// Form is the maker window's fields as plain text and ticks. Fields the window does not show
// (requires, purge, appUserModelId) stay in the spec untouched.
type Form struct {
	Name, Version, ID, Group, Publisher string
	Icon, License                       string
	Files                               []spec.FileRule
	Exclude                             string // patterns, separated by spaces or commas
	Program, Args                       string // Args: a command line ("quoted" arguments may hold spaces)
	AskFolder, AskShortcuts             bool
	StartMenu, Desktop                  bool
	Output                              string
}

// FormFrom fills the form from a spec.
func FormFrom(s *spec.Spec) Form {
	return Form{Name: s.Name, Version: s.Version, ID: s.ID, Group: s.Group, Publisher: s.Publisher,
		Icon: s.Icon, License: s.License, Files: append([]spec.FileRule(nil), s.Files...),
		Exclude: strings.Join(s.Exclude, " "), Program: s.Run.Program, Args: JoinArgs(s.Run.Args),
		AskFolder: s.AskFolder, AskShortcuts: s.Shortcuts.Ask, StartMenu: s.Shortcuts.StartMenu,
		Desktop: s.Shortcuts.Desktop, Output: s.Output}
}

// Apply writes the form into the spec.
func (f Form) Apply(s *spec.Spec) {
	trim := strings.TrimSpace
	s.Name, s.Version, s.ID, s.Group, s.Publisher = trim(f.Name), trim(f.Version), trim(f.ID), trim(f.Group), trim(f.Publisher)
	s.Icon, s.License, s.Output = trim(f.Icon), trim(f.License), trim(f.Output)
	s.Files = append([]spec.FileRule(nil), f.Files...)
	s.Exclude = strings.FieldsFunc(f.Exclude, func(r rune) bool { return r == ' ' || r == ',' || r == ';' })
	s.Run.Program, s.Run.Args = trim(f.Program), SplitArgs(f.Args)
	s.AskFolder = f.AskFolder
	s.Shortcuts = spec.Shortcuts{Ask: f.AskShortcuts, StartMenu: f.StartMenu, Desktop: f.Desktop}
}

// SplitArgs splits a command line into arguments; double quotes keep spaces together.
func SplitArgs(line string) []string {
	var out []string
	var cur strings.Builder
	quoted, have := false, false
	for _, r := range line {
		switch {
		case r == '"':
			quoted, have = !quoted, true
		case (r == ' ' || r == '\t') && !quoted:
			if have {
				out = append(out, cur.String())
				cur.Reset()
				have = false
			}
		default:
			cur.WriteRune(r)
			have = true
		}
	}
	if have {
		out = append(out, cur.String())
	}
	return out
}

// JoinArgs is the opposite of SplitArgs.
func JoinArgs(args []string) string {
	q := make([]string, len(args))
	for i, a := range args {
		if a == "" || strings.ContainsAny(a, " \t") {
			a = `"` + a + `"`
		}
		q[i] = a
	}
	return strings.Join(q, " ")
}

// Relativize rewrites the spec's absolute paths that lie inside dir as relative ones (with "/"), so a
// setup.json saved in dir still works when the folder is moved or checked out elsewhere.
func Relativize(s *spec.Spec, dir string) {
	rel := func(p string) string {
		if !filepath.IsAbs(p) {
			return p
		}
		r, err := filepath.Rel(dir, p)
		if err != nil || !filepath.IsLocal(r) {
			return p
		}
		return filepath.ToSlash(r)
	}
	s.Icon, s.License, s.Output = rel(s.Icon), rel(s.License), rel(s.Output)
	for i := range s.Files {
		s.Files[i].From = rel(s.Files[i].From)
	}
}

// Absolutize makes the spec's relative paths absolute (from its folder), before it moves to another.
func Absolutize(s *spec.Spec) {
	abs := func(p string) string {
		if p == "" || filepath.IsAbs(p) || strings.Contains(p, "{") {
			return p
		}
		return s.Abs(p)
	}
	s.Icon, s.License, s.Output = abs(s.Icon), abs(s.License), abs(s.Output)
	for i := range s.Files {
		s.Files[i].From = abs(s.Files[i].From)
	}
}

// RuleText is how the window lists a file rule.
func RuleText(r spec.FileRule) string {
	to := "the install folder"
	if r.To != "" {
		to = r.To + `\`
	}
	return r.From + "  →  " + to
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=5725b10ebb31d375bb163c3f37024d1504ab6dfce704d261782a9c98e890eb0b ====
