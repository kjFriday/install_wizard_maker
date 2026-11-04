// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/win/menu_windows.go
// About: Menus and keyboard shortcuts: a menu bar for New, and the accelerators that send a...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Menus and keyboard shortcuts: a menu bar for New, and the accelerators that send a window's commands.

package win

import "unsafe"

// Menu is one menu of a menu bar.
type Menu struct {
	Title string
	Items []MenuItem
}

// MenuItem is one entry; ID 0 is a separator.
type MenuItem struct {
	ID   int
	Text string
}

// NewMenu makes a menu bar from items: each top-level item has a title and its entries
// (an entry with ID 0 is a separator).
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
	Key  uint16
	Ctrl bool
	ID   int
}

// Accelerators sets keyboard shortcuts: key is a virtual-key code, ctrl adds Ctrl.
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

// ==== stamp v1 | author=kjFriday | hmac-sha256=957a1f9ee156b59b2dc22d766fdeb74de6b21748154957bff1d34179a71bcf32 ====
