// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/spec/check.go
// About: Checks of a spec: identity, run, exclude and purge entries, Windows names and paths.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Checks of a spec: identity, run, exclude and purge entries, Windows names and paths.
//
// Spec.Check is what the maker's form and build command run before a build; Manifest.Check
// (manifest.go) reuses checkIdentity, checkRun, checkPurge and CheckInstalledPath on a manifest read
// from a setup.exe, and Sources uses CheckInstalledPath on every installed path. Checks collect
// every problem instead of stopping at the first, so the form can show them all at once.
//
// The Windows name rules come from the file-naming conventions in the Win32 documentation: no
// <>:"/\|?* or control characters, no trailing dot or space, and none of the device names (CON,
// PRN, AUX, NUL, COM1-9, LPT1-9 and their superscript-digit forms), with or without an extension.

package spec

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// The shapes of ids, AppUserModelIDs, versions (at most four parts: VS_FIXEDFILEINFO keeps four 16-bit
// numbers) and requires.python.
//
// An AppUserModelID may be up to 128 characters, the limit Windows documents for it; versions allow
// five digits per part so checkIdentity can then report a part above 65535 by name.
var (
	idPattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	aumidPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	versionPattern = regexp.MustCompile(`^\d{1,5}(\.\d{1,5}){0,3}$`)
	pythonPattern  = regexp.MustCompile(`^3\.\d{1,2}$`)
)

// Check reports every problem it finds, one per line, or nil. It also packs nothing: it only looks.
//
// It reads the disk (icon, license and the file rules' sources) but writes nothing. Each problem is
// one error prefixed with the setup.json field it concerns ("id:", "run.program:"); errors.Join
// puts one per line.
//
// Steps:
//  1. Identity: id, name, version, publisher, group.
//  2. The icon and license files.
//  3. run and requires.
//  4. Exclude patterns (path.Match on "" only reports a malformed pattern) and purge entries.
//  5. The output name.
//  6. The file rules, through Sources, and that run.program is one of the packed files.
func (s *Spec) Check() error {
	var errs []error
	add := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }
	for _, e := range checkIdentity(s.ID, s.Name, s.Version, s.Publisher, s.Group) {
		errs = append(errs, e)
	}
	if s.Icon != "" && !isFile(s.Abs(s.Icon)) {
		add("icon: %s not found", s.Icon)
	}
	if s.License != "" {
		if _, err := s.LicenseText(); err != nil {
			add("license: %v", err)
		}
	}
	for _, e := range checkRun(s.Run, s.Requires) {
		errs = append(errs, e)
	}
	for _, p := range s.Exclude {
		if _, err := path.Match(p, ""); err != nil || p == "" {
			add("exclude: bad pattern %q", p)
		}
	}
	for _, p := range s.Purge {
		if err := checkPurge(p); err != nil {
			errs = append(errs, err)
		}
	}
	if _, err := s.OutputPath(); err != nil {
		errs = append(errs, err)
	}
	if len(s.Files) == 0 {
		add("files: nothing to pack")
	} else if src, err := s.Sources(); err != nil {
		errs = append(errs, err)
	} else if s.Run.Program != "" && s.Run.Program != Python && !packed(src, s.Run.Program) {
		add("run.program: %s is not one of the packed files", s.Run.Program)
	}
	return errors.Join(errs...)
}

// packed reports whether p (as written in setup.json, either slash) is one of the packed files'
// installed paths, ignoring case as Windows does.
func packed(src []Source, p string) bool {
	p = strings.Trim(filepath.ToSlash(p), "/")
	for _, s := range src {
		if strings.EqualFold(s.Path, p) {
			return true
		}
	}
	return false
}

// isFile reports whether p exists and is a regular file (not a folder or device).
func isFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular()
}

// checkIdentity checks the fields that name the program, for a spec and a manifest alike.
//
// name and group become folder and shortcut names, so they must be valid Windows names of at most
// 64 characters. Each version part must also fit in 16 bits (65535), the size of a part in the
// exe's VS_FIXEDFILEINFO. The publisher is free text of 1-128 printable characters.
func checkIdentity(id, name, version, publisher, group string) []error {
	var errs []error
	if !idPattern.MatchString(id) {
		errs = append(errs, fmt.Errorf("id: %q should be 1-64 letters, digits, '.', '_' or '-' (e.g. GenericCompany.CMS)", id))
	}
	if err := checkComponent(name, 64); err != nil {
		errs = append(errs, fmt.Errorf("name: %w", err))
	}
	if !versionPattern.MatchString(version) {
		errs = append(errs, fmt.Errorf("version: %q should be numbers like 1.2.0", version))
	} else {
		for _, part := range strings.Split(version, ".") {
			if n := atoi(part); n > 65535 {
				errs = append(errs, fmt.Errorf("version: %s is larger than 65535", part))
			}
		}
	}
	if strings.TrimSpace(publisher) == "" || utf8.RuneCountInString(publisher) > 128 || hasControl(publisher) {
		errs = append(errs, fmt.Errorf("publisher: %q should be 1-128 printable characters", publisher))
	}
	if group != "" {
		if err := checkComponent(group, 64); err != nil {
			errs = append(errs, fmt.Errorf("group: %w", err))
		}
	}
	return errs
}

// checkRun checks what the shortcuts start, for a spec and a manifest alike.
//
// The program must be {python} (which needs requires.python) or a valid installed path; whether it
// is packed is checked by the callers, which know the files. Args are expanded with empty values
// only to catch unknown placeholders and stray braces.
func checkRun(r Run, req Requires) []error {
	var errs []error
	switch {
	case r.Program == "":
		errs = append(errs, errors.New("run.program: missing (the packed file the shortcuts start)"))
	case r.Program == Python:
		if req.Python == "" {
			errs = append(errs, errors.New("run.program: {python} needs requires.python, e.g. \"3.10\""))
		}
	default:
		if err := CheckInstalledPath(strings.Trim(filepath.ToSlash(r.Program), "/")); err != nil {
			errs = append(errs, fmt.Errorf("run.program: %w", err))
		}
	}
	if req.Python != "" && !pythonPattern.MatchString(req.Python) {
		errs = append(errs, fmt.Errorf("requires.python: %q should look like 3.10", req.Python))
	}
	for _, a := range r.Args {
		if _, err := Expand(a, ArgVars("", "", "", "", "")); err != nil {
			errs = append(errs, fmt.Errorf("run.args: %w", err))
		}
	}
	if r.AppUserModelID != "" && !aumidPattern.MatchString(r.AppUserModelID) {
		errs = append(errs, fmt.Errorf("run.appUserModelId: %q should be 1-128 letters, digits, '.', '_' or '-'", r.AppUserModelID))
	}
	return errs
}

// checkPurge checks a purge entry's shape (PurgePath also checks where it lands once expanded).
//
// The shape: {localappdata}\… or {appdata}\…, at least two folders deep, nothing else expanded.
func checkPurge(p string) error {
	parts := strings.FieldsFunc(p, func(r rune) bool { return r == '\\' || r == '/' })
	// three parts: the root placeholder plus two folders; "." and ".." fail checkComponent below
	if len(parts) < 3 || (parts[0] != "{localappdata}" && parts[0] != "{appdata}") {
		return fmt.Errorf("purge: %q should be {localappdata}\\… or {appdata}\\…, at least two folders deep", p)
	}
	for _, c := range parts[1:] {
		if err := checkComponent(c, 100); err != nil {
			return fmt.Errorf("purge: %q: %w", p, err)
		}
	}
	return nil
}

// CheckInstalledPath checks a slash-separated path inside the install folder: every part a valid name,
// not one of the names setup itself writes there, at most 160 characters.
//
// It guards every path a manifest names, since a tampered setup.exe could otherwise write outside
// the install folder. Backslashes, a leading "/", and anything filepath.IsLocal rejects (a drive
// such as "C:", a ".." that climbs out) all fail.
func CheckInstalledPath(p string) error {
	if p == "" {
		return errors.New("empty path")
	}
	if strings.Contains(p, `\`) || strings.HasPrefix(p, "/") || utf8.RuneCountInString(p) > 160 ||
		!filepath.IsLocal(filepath.FromSlash(p)) {
		return errors.New("must be a relative path inside the install folder, at most 160 characters")
	}
	parts := strings.Split(p, "/")
	for _, c := range parts {
		if err := checkComponent(c, 100); err != nil {
			return err
		}
	}
	// setup's own files are reserved only at the top ("www/app.ico" is fine); the .setup work folder,
	// which Apply stages through and deletes as its commit, is reserved with everything under it
	switch strings.ToLower(parts[0]) {
	case "install.json", "uninstall.exe", "app.ico", ".setup":
		if len(parts) == 1 || strings.EqualFold(parts[0], ".setup") {
			return fmt.Errorf("%s is written by setup itself", parts[0])
		}
	}
	return nil
}

// checkComponent checks that c is a valid Windows file or folder name of at most max characters.
//
// max counts runes, not bytes. The error names the first rule c breaks.
func checkComponent(c string, max int) error {
	switch {
	case c == "" || c == "." || c == "..":
		return fmt.Errorf("%q is not a name", c)
	case utf8.RuneCountInString(c) > max:
		return fmt.Errorf("%q is longer than %d characters", c, max)
	case strings.ContainsAny(c, `<>:"/\|?*`) || hasControl(c):
		return fmt.Errorf("%q contains a character Windows does not allow in names", c)
	case strings.HasSuffix(c, ".") || strings.HasSuffix(c, " ") || strings.HasPrefix(c, " "):
		return fmt.Errorf("%q starts with a space or ends with a dot or space", c)
	case reserved(c):
		return fmt.Errorf("%q is a reserved device name", c)
	}
	return nil
}

// reserved reports whether c is a Windows device name, which Windows treats as the device whatever
// the folder or extension ("con.txt" and "LPT1.log" are reserved too).
//
// The part before the first dot, upper-cased with trailing spaces trimmed, is compared, because
// Windows ignores both when it matches device names. COM and LPT also take the superscript digits
// 1-3, which Windows maps to the same ports.
func reserved(c string) bool {
	base := strings.ToUpper(strings.TrimRight(strings.SplitN(c, ".", 2)[0], " "))
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return true
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '0' && base[3] <= '9' {
		return true
	}
	// the superscripts are two bytes each in UTF-8, so the length check works on runes
	r := []rune(base)
	return len(r) == 4 && (string(r[:3]) == "COM" || string(r[:3]) == "LPT") && strings.ContainsRune("¹²³", r[3])
}

// hasControl reports whether s has an ASCII control character (below 0x20, or DEL).
func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=ae1d950be667ce13b83aac7b544f186486b09411d3ce250541931d7820ad34e2 ====
