// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/install/places.go
// About: The folders an install touches: the current user's, or everything under a -root sandbox.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// The folders an install touches: the current user's, or everything under a -root sandbox.

package install

import (
	"path/filepath"
	"strings"
)

// Places are the folders an install touches.
type Places struct {
	Programs     string // per-user programs folder (%LOCALAPPDATA%\Programs)
	StartMenu    string // the Start menu's Programs folder
	Desktop      string
	LocalAppData string // for purge entries
	AppData      string
	Root         string // the -root sandbox; "" on the real computer
}

// SandboxPlaces is the -root layout the other Generic Company programs use: everything under root, and the
// registry in root\registry.json.
func SandboxPlaces(root string) Places {
	root, _ = filepath.Abs(root)
	return Places{Programs: filepath.Join(root, "Programs"), StartMenu: filepath.Join(root, "Start Menu"),
		Desktop: filepath.Join(root, "Desktop"), LocalAppData: filepath.Join(root, "LocalAppData"),
		AppData: filepath.Join(root, "AppData"), Root: root}
}

// Dir is the default install folder: Programs\[group\]name.
func (p Places) Dir(group, name string) string { return filepath.Join(p.Programs, group, name) }

// StartMenuLink is the Start-menu shortcut: [group\]name.lnk.
func (p Places) StartMenuLink(group, name string) string {
	return filepath.Join(p.StartMenu, group, name+".lnk")
}

// DesktopLink is the desktop shortcut.
func (p Places) DesktopLink(name string) string { return filepath.Join(p.Desktop, name+".lnk") }

// ownsLink: shortcuts are only ever .lnk files in the Start menu or on the desktop.
func (p Places) ownsLink(path string) bool {
	if !strings.EqualFold(filepath.Ext(path), ".lnk") {
		return false
	}
	return within(p.StartMenu, path) || within(p.Desktop, path)
}

// within reports whether path is strictly inside dir.
func within(dir, path string) bool {
	if dir == "" {
		return false
	}
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != "." && filepath.IsLocal(rel)
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=fbcf65adc96a58e3f1e51881ec37e6972947f2c0209127172d1d052d8034f66d ====
