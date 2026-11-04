// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/spec/placeholders.go
// About: Placeholders such as {app} and {localappdata}, and the purge paths they expand to.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Placeholders such as {app} and {localappdata}, and the purge paths they expand to.

package spec

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Vars are the values placeholders expand to.
type Vars map[string]string

// ArgVars are the placeholders run.args may use.
func ArgVars(app, name, version, localAppData, appData string) Vars {
	return Vars{"app": app, "name": name, "version": version, "localappdata": localAppData, "appdata": appData}
}

var placeholder = regexp.MustCompile(`\{[^{}]*\}`)

// Expand replaces each {name} with its value. An unknown placeholder, or a stray brace, is an error.
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

// ==== stamp v1 | author=kjFriday | hmac-sha256=a4b12f3136163e0f72290e62d2dc3b4ac4c1fe58ae0495ac89dc8f20f8d30437 ====
