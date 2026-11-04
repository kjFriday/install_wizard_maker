// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/install/places.go
// About: The folders an install touches: the current user's, or everything under a -root sandbox.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// The folders an install touches: the current user's, or everything under a -root sandbox.
//
// The wizard and the maker get Places from shell.UserPlaces (the shell's known folders) on a real
// computer, or from SandboxPlaces for -root, and hand them to MakePlan, Apply, Uninstall, CheckFolder and Existing. Nothing here
// touches the disk: these are path calculations only. ownsLink and within are the safety checks that
// keep an uninstall from deleting anything outside the folders it owns, whatever a marker says.

package install

import (
	"path/filepath"
	"strings"
)

// Places are the folders an install touches. All are absolute paths; an empty one is never matched
// by within, so a missing folder cannot widen what an uninstall may remove.
type Places struct {
	Programs     string // per-user programs folder (%LOCALAPPDATA%\Programs)
	StartMenu    string // the Start menu's Programs folder
	Desktop      string // the user's desktop, for the optional desktop shortcut
	LocalAppData string // for purge entries
	AppData      string // roaming application data, the other root purge entries and {appdata} may use
	Root         string // the -root sandbox; "" on the real computer
}

// SandboxPlaces is the -root layout the other Generic Company programs use: everything under root, and the
// registry in root\registry.json (package reg's File writes that; this only lays out the folders).
// root is made absolute first; on the rare error from filepath.Abs it is used as given.
func SandboxPlaces(root string) Places {
	root, _ = filepath.Abs(root)
	return Places{Programs: filepath.Join(root, "Programs"), StartMenu: filepath.Join(root, "Start Menu"),
		Desktop: filepath.Join(root, "Desktop"), LocalAppData: filepath.Join(root, "LocalAppData"),
		AppData: filepath.Join(root, "AppData"), Root: root}
}

// Dir is the default install folder: Programs\[group\]name. An empty group drops out, because
// filepath.Join skips empty elements.
func (p Places) Dir(group, name string) string { return filepath.Join(p.Programs, group, name) }

// StartMenuLink is the Start-menu shortcut: [group\]name.lnk.
func (p Places) StartMenuLink(group, name string) string {
	return filepath.Join(p.StartMenu, group, name+".lnk")
}

// DesktopLink is the desktop shortcut: Desktop\name.lnk.
func (p Places) DesktopLink(name string) string { return filepath.Join(p.Desktop, name+".lnk") }

// ownsLink: shortcuts are only ever .lnk files in the Start menu or on the desktop. A marker's
// shortcut list is only data, possibly written by another program, so MakePlan and Uninstall ask
// this before they move or delete a listed shortcut. The extension check ignores case.
func (p Places) ownsLink(path string) bool {
	if !strings.EqualFold(filepath.Ext(path), ".lnk") {
		return false
	}
	return within(p.StartMenu, path) || within(p.Desktop, path)
}

// within reports whether path is strictly inside dir: dir itself, a sibling, a parent and anything
// reached through ".." all report false, and so does every path when dir is "".
// The test is lexical (filepath.Rel compares the cleaned strings, ignoring case on Windows); it does
// not follow links.
func within(dir, path string) bool {
	if dir == "" {
		return false
	}
	// IsLocal rejects "..", absolute and empty results; "." would be dir itself
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != "." && filepath.IsLocal(rel)
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=ff8f7419af1e14583d7e74f8144f8d0692c5ed29763778e93707fb357cbecb7b ====
