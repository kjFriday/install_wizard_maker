// ==== ACME Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/python/python.go
// About: Package python finds an installed CPython the way PEP 514 says to: under PythonCore in...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Package python finds an installed CPython the way PEP 514 says to: under PythonCore in the per-user,
// then the all-users registry, the newest version at or above a minimum, and its pythonw.exe.
package python

import (
	"path/filepath"
	"strconv"
	"strings"
)

// Registry reads the keys Python registers; tests pass a fake.
type Registry interface {
	Subkeys(root uintptr, path string) ([]string, error)
	String(root uintptr, path, name string) (string, bool)
}

// Root keys, as the reg package numbers them.
const (
	CurrentUser  = 0x80000001
	LocalMachine = 0x80000002
)

// Found is one usable Python.
type Found struct {
	Version [2]int // major, minor
	Tag     string
	Pythonw string
}

var places = []struct {
	root uintptr
	path string
}{
	{CurrentUser, `Software\Python\PythonCore`},
	{LocalMachine, `Software\Python\PythonCore`},
	{LocalMachine, `Software\WOW6432Node\Python\PythonCore`},
}

// Find returns the newest registered CPython of at least min ("3.10") whose pythonw.exe exists. On a tie
// the per-user one wins (it is listed first).
func Find(reg Registry, min string, exists func(string) bool) (Found, bool) {
	want, ok := parse(min)
	if !ok {
		return Found{}, false
	}
	var best Found
	for _, p := range places {
		tags, _ := reg.Subkeys(p.root, p.path)
		for _, tag := range tags {
			key := p.path + `\` + tag
			v, ok := parse(tag)
			if sys, found := reg.String(p.root, key, "SysVersion"); found {
				v, ok = parse(sys)
			}
			if !ok || less(v, want) || (best.Pythonw != "" && !less(best.Version, v)) {
				continue
			}
			exe, found := reg.String(p.root, key+`\InstallPath`, "WindowedExecutablePath")
			if !found || exe == "" {
				dir, found := reg.String(p.root, key+`\InstallPath`, "")
				if !found || dir == "" {
					continue
				}
				exe = filepath.Join(dir, "pythonw.exe")
			}
			if exists(exe) {
				best = Found{Version: v, Tag: tag, Pythonw: exe}
			}
		}
	}
	return best, best.Pythonw != ""
}

// parse reads the major.minor a tag or version starts with: "3.12", "3.12-32", "3.13t".
func parse(s string) ([2]int, bool) {
	parts := strings.SplitN(s, ".", 3)
	if len(parts) < 2 {
		return [2]int{}, false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return [2]int{}, false
	}
	digits := parts[1]
	for i, c := range digits {
		if c < '0' || c > '9' {
			digits = digits[:i]
			break
		}
	}
	minor, err := strconv.Atoi(digits)
	if err != nil {
		return [2]int{}, false
	}
	return [2]int{major, minor}, true
}

func less(a, b [2]int) bool { return a[0] < b[0] || (a[0] == b[0] && a[1] < b[1]) }

// ==== stamp v1 | author=kjFriday | hmac-sha256=d7e53a1d58b59a326c6d94086659b8061bff15c08b66d6f2ad8d2d7bf7872ad4 ====
