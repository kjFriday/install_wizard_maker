// ==== Generic Company Inc. portfolio ====================================
// Project: install_wizard_maker | File: internal/spec/spec.go
// About: ==== Generic Company Inc. portfolio ===============================================...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// ==== Generic Company Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/spec/spec.go
// About: Package spec reads and checks setup.json, the description of one install wizard, and...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Package spec reads and checks setup.json, the description of one install wizard, and turns it into
// the manifest a setup.exe carries.
//
// Paths in setup.json are relative to the file itself. A file rule copies a file, or a folder's
// contents, into the folder "to" of the install folder (default: the install folder itself).
package spec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// Spec is setup.json.
type Spec struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Version   string     `json:"version"`
	Publisher string     `json:"publisher"`
	Group     string     `json:"group,omitempty"`
	Icon      string     `json:"icon,omitempty"`
	License   string     `json:"license,omitempty"`
	Files     []FileRule `json:"files"`
	Exclude   []string   `json:"exclude,omitempty"`
	Run       Run        `json:"run"`
	Requires  Requires   `json:"requires,omitzero"`
	AskFolder bool       `json:"askFolder"`
	Shortcuts Shortcuts  `json:"shortcuts"`
	Purge     []string   `json:"purge,omitempty"`
	Output    string     `json:"output,omitempty"`

	dir string // the folder setup.json is in
}

// FileRule packs a file, or a folder's contents, into folder To of the install folder.
type FileRule struct {
	From string `json:"from"`
	To   string `json:"to,omitempty"`
}

// Run is what the shortcuts start: a packed file, or {python} with Requires.Python.
type Run struct {
	Program        string   `json:"program"`
	Args           []string `json:"args,omitempty"`
	AppUserModelID string   `json:"appUserModelId,omitempty"`
}

// Requires names software setup must find on the computer.
type Requires struct {
	Python string `json:"python,omitempty"` // lowest CPython version, e.g. "3.10"
}

// Shortcuts: whether setup asks, and which shortcuts are ticked.
type Shortcuts struct {
	Ask       bool `json:"ask"`
	StartMenu bool `json:"startMenu"`
	Desktop   bool `json:"desktop"`
}

// Python is the run.program placeholder for the newest registered CPython's pythonw.exe.
const Python = "{python}"

// New is an empty spec with the defaults: ask for the folder, ask about shortcuts, Start menu ticked.
func New(dir string) *Spec {
	return &Spec{AskFolder: true, Shortcuts: Shortcuts{Ask: true, StartMenu: true}, dir: dir}
}

// Load reads setup.json. Unknown fields are errors, so a typo cannot pass silently.
func Load(file string) (*Spec, error) {
	abs, err := filepath.Abs(file)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	s := New(filepath.Dir(abs))
	dec := json.NewDecoder(bytes.NewReader(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))))
	dec.DisallowUnknownFields()
	if err := dec.Decode(s); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(abs), err)
	}
	return s, nil
}

// Save writes the spec as setup.json (indented, fields in a fixed order) and makes file its folder.
func (s *Spec) Save(file string) error {
	abs, err := filepath.Abs(file)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := abs + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, abs); err != nil {
		os.Remove(tmp)
		return err
	}
	s.dir = filepath.Dir(abs)
	return nil
}

// Dir is the folder relative paths start from.
func (s *Spec) Dir() string { return s.dir }

// SetDir changes the folder relative paths start from.
func (s *Spec) SetDir(dir string) { s.dir = dir }

// Abs resolves a path from setup.json.
func (s *Spec) Abs(p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(s.dir, filepath.FromSlash(p))
}

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

// LicenseText is the license file as text (UTF-8, at most 1 MiB, byte-order mark removed).
func (s *Spec) LicenseText() (string, error) {
	if s.License == "" {
		return "", nil
	}
	b, err := os.ReadFile(s.Abs(s.License))
	if err != nil {
		return "", err
	}
	if len(b) > 1<<20 {
		return "", fmt.Errorf("%s is larger than 1 MiB", s.License)
	}
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	if !utf8.Valid(b) {
		return "", fmt.Errorf("%s is not UTF-8 text", s.License)
	}
	return string(b), nil
}

// OutputPath is where the setup.exe goes: output (default "{name}-{version}-setup.exe") beside setup.json.
func (s *Spec) OutputPath() (string, error) {
	out := s.Output
	if out == "" {
		out = "{name}-{version}-setup.exe"
	}
	name, err := Expand(out, Vars{"name": s.Name, "version": s.Version, "id": s.ID})
	if err != nil {
		return "", fmt.Errorf("output: %w", err)
	}
	if !strings.EqualFold(filepath.Ext(name), ".exe") {
		return "", fmt.Errorf("output: %s should end in .exe", name)
	}
	return s.Abs(name), nil
}

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

func atoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
		if n > 1<<20 {
			return n
		}
	}
	return n
}

// CompareVersions compares dotted numeric versions ("1.10" > "1.9"; missing parts count as 0).
func CompareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x = atoi(pa[i])
		}
		if i < len(pb) {
			y = atoi(pb[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=435d7abe7e7eb5f4c27ac28456a8e5c8b7ca99bb073c24c8f6da3009a1d2f149 ====
