// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/win/control_windows.go
// About: Controls: the native child windows a Window holds, their factories, and their use.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Controls: the native child windows a Window holds, their factories, and their use.
//
// The controls are labels, buttons, check boxes, text boxes, lists and progress bars. Each factory
// is one CreateWindowEx call with a predefined class ("STATIC", "BUTTON", "EDIT", "LISTBOX",
// "COMBOBOX", "msctls_progress32") and a style; Add does the work. Positions are in 96-dpi layout
// units, scaled to the window's DPI, and layout re-places every control when the DPI changes. A
// control tells its window about clicks and edits with WM_COMMAND, which reaches Window.OnCommand.
// The maker's form and the wizard's pages build their screens from these; all of it runs on the UI
// thread.

package win

import (
	"strings"
	"syscall"
	"unsafe"
)

// Control is one child window.
type Control struct {
	HWND  uintptr // the control's window handle
	ID    int     // the command id its WM_COMMAND carries (0 for labels)
	Class string  // the window class it was made with
	Rect  Rect    // where it sits, in 96-dpi units
	Font  Font    // which of the window's fonts it uses
	Look  Look    // what it sits on, which sets its colours
	style uint32  // the style bits given to Add (fits and SetDefault read them)
	w     *Window // the window holding it
}

// Add makes a control. style is added to WS_CHILD | WS_VISIBLE.
//
// exStyle is the WS_EX_* style, id the command id, r the place in 96-dpi units. The control gets
// font f at once and joins the window's list, whose order is the Tab order. A failed
// CreateWindowEx is not reported: HWND is then 0 and later calls on the control do nothing.
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
//
// Without SS_NOPREFIX the letter after "&" is underlined, and IsDialogMessage moves the focus to
// the next control in Tab order when the user presses Alt and that letter.
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
//
// Subclassing puts subclassProc in front of the edit control's own window procedure (subclass id
// 1, no reference data).
func (w *Window) Text(id int, text string, r Rect) *Control {
	c := w.Add("EDIT", "", esMultiline|esReadOnly|esAutoVScroll|wsVScroll|wsTabStop, wsExClientEdge, id, r, Normal, Paper)
	procSetWindowSubclass.Call(c.HWND, subclassCB, 1, 0)
	// EM_SETLIMITTEXT 0 lifts the default limit (32,767 characters) to the largest there is; the
	// limit governs typing and pasting, not text set by the program
	send(c.HWND, emSetLimitText, 0, 0)
	c.SetText(text)
	return c
}

// subclassProc makes a read-only multi-line edit let go of Tab, Enter and Esc, as in a dialog.
//
// IsDialogMessage asks a control with WM_GETDLGCODE which keys it wants. A multi-line edit answers
// DLGC_WANTALLKEYS (so Enter and Tab would stay in it) and DLGC_HASSETSEL (so tabbing in would
// select all its text). Every message goes to the edit first (DefSubclassProc); for that one
// question the two bits are taken out of its answer. id and ref are the subclass id and reference
// data, unused.
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
//
// The id is what wndProc answers to DM_GETDEFID, which IsDialogMessage asks when Enter is pressed.
// Every push button is restyled so only the default one wears the thick default border.
func (w *Window) SetDefault(id int) {
	w.defID = id
	// push buttons only: BS_PUSHBUTTON (0) and BS_DEFPUSHBUTTON (1); check boxes are type 3
	for _, c := range w.controls {
		if c.Class == "BUTTON" && c.style&typeMask <= bsDefPushButton {
			style := uintptr(bsPushButton)
			if c.ID == id {
				style = bsDefPushButton
			}
			send(c.HWND, bmSetStyle, style, 1) // lParam 1: redraw now
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
//
// The font is sent again because the window remakes its fonts at the new DPI; both calls repaint.
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

// windowText is any window's text (a control's caption or contents).
//
// The buffer is the reported length plus one for the NUL. GetWindowTextLength may report more
// than the text holds but never less, so nothing is cut; the NUL ends the string.
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

// Checked reports whether a check box is ticked (BM_GETCHECK answers BST_CHECKED, 1).
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
//
// PBM_SETRANGE32 takes the range as two 32-bit numbers (0 to max), so file counts or byte totals
// past 65535 still fit; the range is set before the position, which the bar clamps to it.
func (c *Control) SetProgress(pos, max int) {
	send(c.HWND, pbmSetRange32, 0, uintptr(max))
	send(c.HWND, pbmSetPos, uintptr(pos), 0)
}

// SetItems replaces a list box's or combo box's lines.
//
// The two classes use different message numbers for the same jobs, so the pair is picked by class.
// The lines are added in order (neither control is made with a sorting style).
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
//
// LB_ERR (-1) comes back as a 32-bit value in a 64-bit result, so it goes through int32 to keep
// its sign.
func (c *Control) Selected() int { return int(int32(send(c.HWND, lbGetCurSel, 0, 0))) }

// Select selects a list box line (-1 for none).
func (c *Control) Select(i int) { send(c.HWND, lbSetCurSel, uintptr(i), 0) }

// SelectAll selects an edit box's whole text (EM_SETSEL from 0 to -1, which means the end).
func (c *Control) SelectAll() { send(c.HWND, emSetSel, 0, ^uintptr(0)) }

// ==== stamp v1 | author=kjFriday | hmac-sha256=8bfd78df8714143ff3c8d1845f69740ee2ccdd6be5aff154c69b1785d8f60bc3 ====
