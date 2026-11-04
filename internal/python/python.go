// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/python/python.go
// About: Package python finds an installed CPython through the registry keys PEP 514 describes.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Package python finds an installed CPython through the registry keys PEP 514 describes.
//
// It looks under PythonCore in the per-user, then the all-users registry, for the newest version at
// or above a minimum, and returns its pythonw.exe.
//
// PEP 514 in short: every Python installer registers itself under
// Software\Python\<Company>\<Tag>, in HKEY_CURRENT_USER for a per-user install or
// HKEY_LOCAL_MACHINE for an all-users one. CPython's company is PythonCore and its tag is the version
// with suffixes such as "3.12", "3.12-32" (32-bit) or "3.13t" (free-threaded). The tag key may hold
// SysVersion ("3.12"), and its InstallPath subkey holds the folder as the default value and,
// in newer installs, ExecutablePath and WindowedExecutablePath (pythonw.exe, the console-less
// interpreter a GUI program wants). 32-bit Pythons on 64-bit Windows land under WOW6432Node.
//
// Callers: the setup wizard checks a spec's requires.python with Find (on Windows, the real
// registry), and the tests pass a fake Registry. Start reading at Find.
package python

import (
	"path/filepath"
	"strconv"
	"strings"
)

// Registry reads the keys Python registers; tests pass a fake.
//
// Subkeys lists the subkey names of root\path (none, without error, when it is missing). String
// reads a string value, name "" being the key's default value; ok is false when it is missing or
// not a string.
type Registry interface {
	Subkeys(root uintptr, path string) ([]string, error)
	String(root uintptr, path, name string) (string, bool)
}

// Root keys, as the reg package numbers them. These are the Win32 predefined handles
// HKEY_CURRENT_USER and HKEY_LOCAL_MACHINE; this file repeats them so it builds on any system.
const (
	CurrentUser  = 0x80000001
	LocalMachine = 0x80000002
)

// Found is one usable Python.
type Found struct {
	Version [2]int // major, minor
	Tag     string // the PEP 514 tag key it was found under, e.g. "3.12" or "3.13t"
	Pythonw string // full path of its pythonw.exe
}

// places are the PythonCore keys searched, in order of preference: per-user, then all-users
// 64-bit, then all-users 32-bit (the WOW6432Node view). Find keeps the first of equal versions, so
// this order breaks ties.
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
//
// exists reports whether a path is there (the caller decides; on Windows, an existing non-folder).
// ok is false when min does not parse or no registered Python qualifies.
//
// Steps:
//  1. For each place, list the tags under PythonCore.
//  2. Take the version from SysVersion if present, else from the tag itself; skip unparsable ones,
//     ones below min, and ones not newer than the best so far.
//  3. Find pythonw.exe: InstallPath's WindowedExecutablePath, else pythonw.exe in InstallPath's
//     default value (older installs only write the folder).
//  4. Keep it as the best if the file exists.
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
//
// A patch level ("3.10.4") is ignored. The minor part is the digits up to the first non-digit, so
// suffixes such as "-32" or "t" drop off; ok is false without a dot or without digits on either side.
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

// less reports whether version a (major, minor) is older than b.
func less(a, b [2]int) bool { return a[0] < b[0] || (a[0] == b[0] && a[1] < b[1]) }

// ==== stamp v1 | author=kjFriday | hmac-sha256=33ac89ca3c1b7d5f47771af29ddcd1c21cc83850627b90b12014c413febe365c ====
