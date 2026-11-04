// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/spec/check.go
// About: Checks of a spec: its identity, run, exclude and purge entries, and Windows names and...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Checks of a spec: its identity, run, exclude and purge entries, and Windows names and paths.

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
var (
	idPattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	aumidPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	versionPattern = regexp.MustCompile(`^\d{1,5}(\.\d{1,5}){0,3}$`)
	pythonPattern  = regexp.MustCompile(`^3\.\d{1,2}$`)
)

// Check reports every problem it finds, one per line, or nil. It also packs nothing: it only looks.
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

func packed(src []Source, p string) bool {
	p = strings.Trim(filepath.ToSlash(p), "/")
	for _, s := range src {
		if strings.EqualFold(s.Path, p) {
			return true
		}
	}
	return false
}

func isFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular()
}

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

// checkPurge: {localappdata}\… or {appdata}\…, at least two folders deep, nothing else expanded.
func checkPurge(p string) error {
	parts := strings.FieldsFunc(p, func(r rune) bool { return r == '\\' || r == '/' })
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
	switch strings.ToLower(parts[0]) {
	case "install.json", "uninstall.exe", "app.ico", ".setup":
		if len(parts) == 1 || strings.EqualFold(parts[0], ".setup") {
			return fmt.Errorf("%s is written by setup itself", parts[0])
		}
	}
	return nil
}

// checkComponent: a valid Windows file or folder name of at most max characters.
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

func reserved(c string) bool {
	base := strings.ToUpper(strings.TrimRight(strings.SplitN(c, ".", 2)[0], " "))
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return true
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '0' && base[3] <= '9' {
		return true
	}
	r := []rune(base)
	return len(r) == 4 && (string(r[:3]) == "COM" || string(r[:3]) == "LPT") && strings.ContainsRune("¹²³", r[3])
}

func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=9a881d1298deeec0c75d9610411df835bfb33c419c8c109074cb05d0d3660058 ====
