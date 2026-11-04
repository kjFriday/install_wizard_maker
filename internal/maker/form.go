// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/maker/form.go
// About: The maker window's form as plain data, and its conversions to and from a spec.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// The maker window's form as plain data, and its conversions to and from a spec.
//
// Keeping these free of Win32 lets them be tested without a window (form_test.go). The window
// (window_windows.go) fills its controls with FormFrom, writes them back with Apply, and passes
// the spec through Absolutize and Relativize when it saves to another folder, so paths stay
// relative to wherever setup.json is saved.

package maker

import (
	"path/filepath"
	"strings"

	"generic-company/wizard/internal/spec"
)

// Form is the maker window's fields as plain text and ticks. Fields the window does not show
// (requires, purge, appUserModelId) stay in the spec untouched.
// FormFrom and Apply convert both ways; a round trip keeps the spec's values, apart from trimming
// spaces and the separators in Exclude.
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

// FormFrom fills the form from a spec. Files is copied, so editing the form leaves s alone; Exclude
// is joined with spaces and run.args with JoinArgs.
func FormFrom(s *spec.Spec) Form {
	return Form{Name: s.Name, Version: s.Version, ID: s.ID, Group: s.Group, Publisher: s.Publisher,
		Icon: s.Icon, License: s.License, Files: append([]spec.FileRule(nil), s.Files...),
		Exclude: strings.Join(s.Exclude, " "), Program: s.Run.Program, Args: JoinArgs(s.Run.Args),
		AskFolder: s.AskFolder, AskShortcuts: s.Shortcuts.Ask, StartMenu: s.Shortcuts.StartMenu,
		Desktop: s.Shortcuts.Desktop, Output: s.Output}
}

// Apply writes the form into the spec.
// Text fields are trimmed; Exclude splits on spaces, commas and semicolons; Args splits with
// SplitArgs. Only the fields Form has are written, so the others in s survive. It does not
// validate: spec.Check does that at build time.
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
// This is a simple form of the Windows rules: spaces and tabs separate, a quote toggles quoting and
// is dropped, and "" makes an empty argument. Backslashes are plain characters (no \" escape), so
// Windows paths need no doubling; an argument cannot hold a double quote.
func SplitArgs(line string) []string {
	var out []string
	var cur strings.Builder
	// have: an argument has started, even if empty (a "" pair), so it is kept at the next space
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
// An argument that is empty or holds a space or tab is quoted; SplitArgs(JoinArgs(a)) gives a back
// for any arguments without a double quote.
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
// It covers the icon, license, output and each file rule's From; a path outside dir (or on another
// drive) stays absolute.
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
// The window calls it, then Relativize against the new folder, when it saves setup.json into a
// different folder (or saves a new spec for the first time).
func Absolutize(s *spec.Spec) {
	abs := func(p string) string {
		// a path with a {placeholder} (such as output's {name}) is left as written: only the
		// build expands it
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

// RuleText is how the window lists a file rule: `from  →  to\`, or `from  →  the install folder`
// when the rule has no To.
func RuleText(r spec.FileRule) string {
	to := "the install folder"
	if r.To != "" {
		to = r.To + `\`
	}
	return r.From + "  →  " + to
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=6c762d17ca7b5805fc363893e4c0869873a6b9f0315a3f5c8f600d99424b32cc ====
