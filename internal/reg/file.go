// ==== Generic Company Inc. portfolio ====================================
// Project: install_wizard_maker | File: internal/reg/file.go
// About: ==== Generic Company Inc. portfolio ===============================================...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// ==== Generic Company Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/reg/file.go
// About: Package reg is the Apps & features entry: the per-user Uninstall key in the real...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Package reg is the Apps & features entry: the per-user Uninstall key in the real registry, or its JSON
// twin in a -root sandbox. It also reads the keys Python registers (PEP 514).
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
type File struct{ Path string }

func (f File) name(id string) string { return `HKCU:\` + install.UninstallKey(id) }

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

// Write replaces the key's values.
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

// Delete removes the key.
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

// ==== stamp v1 | author=kjFriday | hmac-sha256=4797523be9a6bf3112dc9c682147b6a41bb565f8824cc7283472961e215742c2 ====
