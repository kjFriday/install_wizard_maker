// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/win/menu_windows.go
// About: Menus and keyboard shortcuts: a menu bar for New, and accelerators for commands.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Menus and keyboard shortcuts: a menu bar for New, and accelerators for commands.
//
// The maker's form window has the menu bar and the shortcuts (F5 builds, say). A menu item and its
// shortcut carry the same command id, so both arrive in Window.OnCommand as one WM_COMMAND, the
// same as the matching button. An accelerator table maps a key (with Ctrl or not) to a command id;
// the window's message loop runs TranslateAccelerator on each key message before anything else.

package win

import "unsafe"

// Menu is one menu of a menu bar.
type Menu struct {
	Title string     // the bar's label; "&" marks the Alt mnemonic
	Items []MenuItem // the entries, top to bottom
}

// MenuItem is one entry; ID 0 is a separator.
type MenuItem struct {
	ID   int    // the command id sent as WM_COMMAND
	Text string // the label; a tab followed by text shows a shortcut on the right
}

// NewMenu makes a menu bar from items: each top-level item has a title and its entries
// (an entry with ID 0 is a separator).
//
// The result is an HMENU for New. Once a window has the bar, Windows destroys it with the window;
// failed calls are not reported.
//
// Steps:
//  1. Make the empty bar.
//  2. For each menu, make a popup and append its entries (MF_STRING with the id, or MF_SEPARATOR).
//  3. Append the popup to the bar under its title (MF_POPUP passes the popup's handle as the id).
func NewMenu(menus []Menu) uintptr {
	bar, _, _ := procCreateMenu.Call()
	for _, m := range menus {
		pop, _, _ := procCreatePopupMenu.Call()
		for _, e := range m.Items {
			if e.ID == 0 {
				procAppendMenu.Call(pop, mfSeparator, 0, 0)
				continue
			}
			procAppendMenu.Call(pop, mfString, uintptr(e.ID), uintptr(unsafe.Pointer(utf16(e.Text))))
		}
		procAppendMenu.Call(bar, mfPopup, pop, uintptr(unsafe.Pointer(utf16(m.Title))))
	}
	return bar
}

// Accel is one keyboard shortcut for a command id.
type Accel struct {
	Key  uint16 // virtual-key code: VKF5, or a capital letter or digit as its own character
	Ctrl bool   // Ctrl must be held
	ID   int    // the command id sent as WM_COMMAND (kept to 16 bits)
}

// Accelerators sets keyboard shortcuts: key is a virtual-key code, ctrl adds Ctrl.
//
// It replaces any earlier table: the old one is destroyed and a new one made from keys (FVIRTKEY,
// so Key is a virtual-key code rather than a character). The window destroys the last table when
// it closes.
//
// Known limitation: an empty keys after an earlier table destroys that table but leaves w.accel
// holding its stale handle, which the message loop then still passes to TranslateAccelerator.
func (w *Window) Accelerators(keys []Accel) {
	if w.accel != 0 {
		procDestroyAcceleratorTable.Call(w.accel)
	}
	table := make([]accel, len(keys))
	for i, k := range keys {
		table[i] = accel{virt: fVirtKey, key: k.Key, cmd: uint16(k.ID)}
		if k.Ctrl {
			table[i].virt |= fControl
		}
	}
	if len(table) > 0 {
		w.accel, _, _ = procCreateAcceleratorTable.Call(uintptr(unsafe.Pointer(&table[0])), uintptr(len(table)))
	}
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=fdfad45e42a2e9f3ccbe7c6dcf881a339452f13d7bfa2e892927d193e1827712 ====
