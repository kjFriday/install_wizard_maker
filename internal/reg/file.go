// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/reg/file.go
// About: Package reg stores the Apps & features entry, in the registry or a sandbox JSON file.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Package reg stores the Apps & features entry, in the registry or a sandbox JSON file.
//
// The entry is the per-user Uninstall key in the real registry, or its JSON twin in a -root
// sandbox. It also reads the keys Python registers (PEP 514).
//
// Apps & features (and the older Programs and Features) lists every subkey of
// HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Uninstall, plus the machine-wide
// one. Each subkey holds values such as DisplayName, DisplayVersion, Publisher, InstallLocation,
// EstimatedSize (a DWORD, in KiB), NoModify and UninstallString, the command the Uninstall button runs.
// Package install decides the values; this package only stores them.
//
// Two implementations of install.Registry:
//   - File (this file): root\registry.json, used under -root so tests and trials never touch the
//     real registry. Portable.
//   - HKCU (hkcu_windows.go): the real key, through advapi32. Windows only, with Subkeys and String
//     for package python.
package reg

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"generic-company/wizard/internal/install"
)

// File is the sandbox registry, root\registry.json, in the other Generic Company programs' format: one object per
// key, named "HKCU:\Software\...\Uninstall\<id>", strings as strings and DWORDs as numbers.
//
// The "HKCU:\" prefix is PowerShell's registry drive syntax, the form the sibling programs write.
// Values of other registry types are not kept: Read skips anything that is neither a string nor a
// number, and Write stores them as their (empty) Text. Each call reads and rewrites the whole file;
// nothing locks it, so two processes writing at once can lose one update.
type File struct{ Path string }

// name is the JSON object name of id's key.
func (f File) name(id string) string { return `HKCU:\` + install.UninstallKey(id) }

// load reads the whole file as key name -> value name -> value. A missing or empty file is an empty
// registry; malformed JSON is an error.
func (f File) load() (map[string]map[string]any, error) {
	data := map[string]map[string]any{}
	b, err := os.ReadFile(f.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return data, nil
	}
	if err != nil {
		return nil, err
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &data); err != nil {
			return nil, err
		}
	}
	return data, nil
}

// save writes the whole registry back, indented, creating the folder if needed.
//
// It writes registry.json.tmp first and renames it over the file, so a crash mid-write leaves the
// old file whole rather than a cut-off one.
func (f File) save(data map[string]map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	tmp := f.Path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, f.Path)
}

// Read returns the key's values, sorted by name.
//
// found is false, without error, when there is no such key. JSON numbers arrive as float64 and
// become DWORDs; strings become REG_SZ values.
func (f File) Read(id string) ([]install.Value, bool, error) {
	data, err := f.load()
	if err != nil {
		return nil, false, err
	}
	key, ok := data[f.name(id)]
	if !ok {
		return nil, false, nil
	}
	var out []install.Value
	for name, v := range key {
		switch x := v.(type) {
		case string:
			out = append(out, install.Str(name, x))
		case float64:
			out = append(out, install.DWord(name, uint32(x)))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, true, nil
}

// Write replaces the key's values; other keys in the file are kept as they were.
func (f File) Write(id string, vals []install.Value) error {
	data, err := f.load()
	if err != nil {
		return err
	}
	key := map[string]any{}
	for _, v := range vals {
		if v.Type == install.RegDWORD {
			key[v.Name] = v.Number
		} else {
			key[v.Name] = v.Text
		}
	}
	data[f.name(id)] = key
	return f.save(data)
}

// Delete removes the key; no error, and no write, when it is not there.
func (f File) Delete(id string) error {
	data, err := f.load()
	if err != nil {
		return err
	}
	if _, ok := data[f.name(id)]; !ok {
		return nil
	}
	delete(data, f.name(id))
	return f.save(data)
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=de5b2024fa6bb381d7cb3841937beacad4791be73447a9b853d898521a847dab ====
