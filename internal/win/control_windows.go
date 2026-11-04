// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/win/control_windows.go
// About: Controls: the native child windows a Window holds (labels, buttons, check boxes, text...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Controls: the native child windows a Window holds (labels, buttons, check boxes, text boxes, lists,
// progress bars), the factories that make them, and what can be done with them.

package win

import (
	"strings"
	"syscall"
	"unsafe"
)

// Control is one child window.
type Control struct {
	HWND  uintptr
	ID    int
	Class string
	Rect  Rect
	Font  Font
	Look  Look
	style uint32
	w     *Window
}

// Add makes a control. style is added to WS_CHILD | WS_VISIBLE.
func (w *Window) Add(class, text string, style, exStyle uint32, id int, r Rect, f Font, look Look) *Control {
	c := &Control{ID: id, Class: class, Rect: r, Font: f, Look: look, style: style, w: w}
	c.HWND, _, _ = procCreateWindowEx.Call(uintptr(exStyle), uintptr(unsafe.Pointer(utf16(class))), uintptr(unsafe.Pointer(utf16(text))),
		uintptr(wsChild|wsVisible|style), uintptr(w.Scale(r.X)), uintptr(w.Scale(r.Y)), uintptr(w.Scale(r.W)), uintptr(w.Scale(r.H)),
		w.HWND, uintptr(id), instance(), 0)
	send(c.HWND, wmSetFont, w.fonts[f], 1)
	w.controls = append(w.controls, c)
	return c
}

// Label is static text that wraps; no & mnemonics.
func (w *Window) Label(text string, r Rect, f Font, look Look) *Control {
	return w.Add("STATIC", text, ssLeft|ssNoPrefix, 0, 0, r, f, look)
}

// PathLabel is one line holding a path; a path too long for it loses its middle (C:\Users\…\App).
func (w *Window) PathLabel(text string, r Rect, look Look) *Control {
	return w.Add("STATIC", text, ssLeftNoWrap|ssPathEllipsis|ssNoPrefix, 0, 0, r, Normal, look)
}

// Caption is static text with an & mnemonic for the control after it (a field's label).
func (w *Window) Caption(text string, r Rect, look Look) *Control {
	return w.Add("STATIC", text, ssLeftNoWrap, 0, 0, r, Normal, look)
}

// Button is a push button.
func (w *Window) Button(id int, text string, r Rect) *Control {
	return w.Add("BUTTON", text, bsPushButton|wsTabStop, 0, id, r, Normal, Face)
}

// Check is a check box.
func (w *Window) Check(id int, text string, r Rect, look Look) *Control {
	return w.Add("BUTTON", text, bsAutoCheckBox|wsTabStop, 0, id, r, Normal, look)
}

// Edit is a one-line text box.
func (w *Window) Edit(id int, text string, r Rect) *Control {
	return w.Add("EDIT", text, esAutoHScroll|wsTabStop, wsExClientEdge, id, r, Normal, Face)
}

// Text is a read-only, scrolling, multi-line text box (a license, a log). Tab, Enter and Esc work as in
// a dialog; text has no length limit.
func (w *Window) Text(id int, text string, r Rect) *Control {
	c := w.Add("EDIT", "", esMultiline|esReadOnly|esAutoVScroll|wsVScroll|wsTabStop, wsExClientEdge, id, r, Normal, Paper)
	procSetWindowSubclass.Call(c.HWND, subclassCB, 1, 0)
	send(c.HWND, emSetLimitText, 0, 0)
	c.SetText(text)
	return c
}

// subclassProc makes a read-only multi-line edit let go of Tab, Enter and Esc, as in a dialog.
func subclassProc(hwnd, m, wparam, lparam, id, ref uintptr) uintptr {
	r, _, _ := procDefSubclassProc.Call(hwnd, m, wparam, lparam)
	if uint32(m) == wmGetDlgCode {
		r &^= dlgcWantAllKeys | dlgcHasSetSel
	}
	return r
}

// Progress is a progress bar.
func (w *Window) Progress(r Rect) *Control {
	return w.Add("msctls_progress32", "", 0, 0, 0, r, Normal, Face)
}

// Separator is an etched horizontal line.
func (w *Window) Separator(r Rect) *Control {
	return w.Add("STATIC", "", ssEtchedHorz, 0, 0, r, Normal, Face)
}

// ListBox is a list of lines.
func (w *Window) ListBox(id int, r Rect) *Control {
	return w.Add("LISTBOX", "", lbsNotify|lbsNoIntegral|wsVScroll|wsHScroll|wsTabStop, wsExClientEdge, id, r, Normal, Face)
}

// Combo is a text box with a drop-down list of suggestions.
func (w *Window) Combo(id int, r Rect) *Control {
	return w.Add("COMBOBOX", "", cbsDropDown|cbsAutoHScroll|wsVScroll|wsTabStop, 0, id, r, Normal, Face)
}

// Controls are the window's controls, in the order they were made (the Tab order).
func (w *Window) Controls() []*Control { return w.controls }

// SetDefault makes button id the default (Enter presses it).
func (w *Window) SetDefault(id int) {
	w.defID = id
	for _, c := range w.controls {
		if c.Class == "BUTTON" && c.style&typeMask <= bsDefPushButton {
			style := uintptr(bsPushButton)
			if c.ID == id {
				style = bsDefPushButton
			}
			send(c.HWND, bmSetStyle, style, 1)
		}
	}
}

// control is the window's control with this handle, or nil.
func (w *Window) control(hwnd uintptr) *Control {
	for _, c := range w.controls {
		if c.HWND == hwnd {
			return c
		}
	}
	return nil
}

// layout places every control again, after the window's DPI changed.
func (w *Window) layout() {
	for _, c := range w.controls {
		c.place()
	}
}

// place moves and sizes the control for the window's scale, and gives it its font again.
func (c *Control) place() {
	w := c.w
	procMoveWindow.Call(c.HWND, uintptr(w.Scale(c.Rect.X)), uintptr(w.Scale(c.Rect.Y)), uintptr(w.Scale(c.Rect.W)), uintptr(w.Scale(c.Rect.H)), 1)
	send(c.HWND, wmSetFont, w.fonts[c.Font], 1)
}

// SetText sets a control's text; line ends become CRLF, as edit controls need.
func (c *Control) SetText(s string) {
	if c.Class == "EDIT" {
		s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")
	}
	procSetWindowText.Call(c.HWND, uintptr(unsafe.Pointer(utf16(s))))
}

// TextValue is a control's text.
func (c *Control) TextValue() string { return windowText(c.HWND) }

func windowText(h uintptr) string {
	n, _, _ := procGetWindowTextLength.Call(h)
	buf := make([]uint16, n+1)
	procGetWindowText.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}

// Show shows or hides a control.
func (c *Control) Show(on bool) {
	cmd := uintptr(swHide)
	if on {
		cmd = swShow
	}
	procShowWindow.Call(c.HWND, cmd)
}

// Visible reports whether the control is shown.
func (c *Control) Visible() bool { r, _, _ := procIsWindowVisible.Call(c.HWND); return r != 0 }

// Enable enables or disables a control.
func (c *Control) Enable(on bool) {
	v := uintptr(0)
	if on {
		v = 1
	}
	procEnableWindow.Call(c.HWND, v)
}

// Enabled reports whether the control is enabled.
func (c *Control) Enabled() bool { r, _, _ := procIsWindowEnabled.Call(c.HWND); return r != 0 }

// Checked reports whether a check box is ticked.
func (c *Control) Checked() bool { return send(c.HWND, bmGetCheck, 0, 0) == 1 }

// SetChecked ticks or clears a check box.
func (c *Control) SetChecked(on bool) {
	v := uintptr(0)
	if on {
		v = 1
	}
	send(c.HWND, bmSetCheck, v, 0)
}

// Click presses a button as the user would (the window hears BN_CLICKED).
func (c *Control) Click() { send(c.HWND, bmClick, 0, 0) }

// Focus moves the keyboard focus to the control.
func (c *Control) Focus() { procSetFocus.Call(c.HWND) }

// SetProgress sets a progress bar to pos out of max.
func (c *Control) SetProgress(pos, max int) {
	send(c.HWND, pbmSetRange32, 0, uintptr(max))
	send(c.HWND, pbmSetPos, uintptr(pos), 0)
}

// SetItems replaces a list box's or combo box's lines.
func (c *Control) SetItems(items []string) {
	reset, add := uint32(lbResetContent), uint32(lbAddString)
	if c.Class == "COMBOBOX" {
		reset, add = cbResetContent, cbAddString
	}
	send(c.HWND, reset, 0, 0)
	for _, s := range items {
		send(c.HWND, add, 0, uintptr(unsafe.Pointer(utf16(s))))
	}
}

// Selected is a list box's selected line, or -1.
func (c *Control) Selected() int { return int(int32(send(c.HWND, lbGetCurSel, 0, 0))) }

// Select selects a list box line (-1 for none).
func (c *Control) Select(i int) { send(c.HWND, lbSetCurSel, uintptr(i), 0) }

// SelectAll selects an edit box's whole text.
func (c *Control) SelectAll() { send(c.HWND, emSetSel, 0, ^uintptr(0)) }

// ==== stamp v1 | author=kjFriday | hmac-sha256=eeab6f44d9bfbcf1c69f4e00a0dced54e7bc5695f4b3bcf2cc22fa5035f2a113 ====
