// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/spec/placeholders.go
// About: Placeholders such as {app} and {localappdata}, and the purge paths they expand to.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Placeholders such as {app} and {localappdata}, and the purge paths they expand to.
//
// A placeholder is a name in braces. run.args may use {app} (the install folder), {name},
// {version}, {localappdata} and {appdata}; the output name may use {name}, {version} and {id}.
// Check expands run.args with empty values to catch mistakes early; package install and the wizard
// expand them for real when they write shortcuts. Purge entries start with {localappdata} or
// {appdata}, and the uninstaller turns them into folders with PurgePath.

package spec

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Vars are the values placeholders expand to.
//
// Keys are the names without braces ("app", not "{app}").
type Vars map[string]string

// ArgVars are the placeholders run.args may use.
//
// app is the install folder; localAppData and appData are the user's local and roaming
// application-data folders.
func ArgVars(app, name, version, localAppData, appData string) Vars {
	return Vars{"app": app, "name": name, "version": version, "localappdata": localAppData, "appdata": appData}
}

// placeholder matches one {name}: an opening brace, anything but braces, a closing brace.
var placeholder = regexp.MustCompile(`\{[^{}]*\}`)

// Expand replaces each {name} with its value. An unknown placeholder, or a stray brace, is an error.
//
// There is no escape for a literal brace. Values are not re-scanned, so a value that holds braces
// is inserted as it is. On error the partly expanded string still comes back; the first unknown
// placeholder is the one reported.
//
// Steps:
//  1. Replace every match, noting the first name Vars lacks (it expands to "").
//  2. Remove every match from the original; any brace left over has no partner ("{app", "{{app}}").
func Expand(s string, v Vars) (string, error) {
	var bad error
	out := placeholder.ReplaceAllStringFunc(s, func(m string) string {
		val, ok := v[m[1:len(m)-1]]
		if !ok && bad == nil {
			bad = fmt.Errorf("unknown placeholder %s", m)
		}
		return val
	})
	if bad == nil && strings.ContainsAny(placeholder.ReplaceAllString(s, ""), "{}") {
		bad = fmt.Errorf("%q has a stray { or }", s)
	}
	return out, bad
}

// PurgePath expands a purge entry. The result must lie at least two folders deep inside localAppData or
// appData, so no entry can ever name one of those folders, or anything outside them.
//
// The uninstaller calls it for each purge entry when the user asks to remove the program's data,
// and skips an entry that fails. A root that is "" here (unknown on this computer) is an error.
//
// Steps:
//  1. checkPurge vets the shape: a root placeholder plus two valid folder names.
//  2. Join the parts under the chosen root.
//  3. As a second guard, filepath.Rel must show the result inside the root, two or more levels down.
func PurgePath(p, localAppData, appData string) (string, error) {
	if err := checkPurge(p); err != nil {
		return "", err
	}
	parts := strings.FieldsFunc(p, func(r rune) bool { return r == '\\' || r == '/' })
	base := localAppData
	if parts[0] == "{appdata}" {
		base = appData
	}
	if base == "" {
		return "", fmt.Errorf("purge: %s has no folder here", parts[0])
	}
	out := filepath.Join(append([]string{base}, parts[1:]...)...)
	rel, err := filepath.Rel(base, out)
	if err != nil || !filepath.IsLocal(rel) || len(strings.Split(rel, string(filepath.Separator))) < 2 {
		return "", fmt.Errorf("purge: %q is not inside %s", p, base)
	}
	return out, nil
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=f0c3428e8eac071b6b38d79d9c774014b771d21c6696c6685093dbef32163e72 ====
