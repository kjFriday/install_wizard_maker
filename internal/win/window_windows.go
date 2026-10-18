// ==== ACME Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/win/window_windows.go
// About: Part of package win in install_wizard_maker.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

package win

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// Rect is a rectangle in 96-dpi units; the window scales it to its monitor's DPI.
type Rect struct{ X, Y, W, H int }

// Font is one of a window's three fonts, all from the system's message font (Segoe UI).
type Font int

const (
	Normal Font = iota
	Bold
	Title // bold and half again as large
	fontCount
)

// Look is what a control (or a painted area) sits on.
type Look int

const (
	Face   Look = iota // the dialog colour
	Paper              // white: page headers, welcome and finish pages
	Banner             // the welcome and finish pages' side banner: dark, white text
)

// BannerColor is the side banner's colour (COLORREF, 0x00BBGGRR): a dark ACME blue.
const BannerColor = 0x005B3923

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

// Window is a fixed-size top-level window holding controls.
type Window struct {
	HWND     uintptr
	W, H     int // client size in 96-dpi units
	dpi      uint32
	text     float64 // the user's text size (Settings > Make text bigger): 1 = 100 %
	style    uint32
	exStyle  uint32
	menu     uintptr
	controls []*Control
	fonts    [fontCount]uintptr
	banner   uintptr
	accel    uintptr
	defID    int
	waking   atomic.Bool
	closed   bool

	OnCommand func(id, code int) // a button, menu item or control notification
	OnClose   func() bool        // the close box or Alt+F4; false keeps the window open
	OnPaint   func(p *Painter)   // draw the background areas
	OnWake    func()             // after Wake, on the UI thread
	OnDPI     func()             // after the window moved to a monitor with another DPI
	Panic     error              // the first panic a handler raised (recovered, so the window lives on)
}

var (
	classOnce  sync.Once
	classErr   error
	windows    = map[uintptr]*Window{}
	creating   *Window
	wndProcCB  = syscall.NewCallback(wndProc)
	subclassCB = syscall.NewCallback(subclassProc)
)

const className = "ACMEWizardWindow"

func register() error {
	icc := [2]uint32{8, 0x00FF | 0x4000} // ICC_WIN95_CLASSES | ICC_STANDARD_CLASSES
	procInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))
	dpi := systemDPI()
	icon := func(metric uintptr) uintptr {
		size, _, _ := procGetSystemMetricsForDpi.Call(metric, uintptr(dpi))
		h, _, _ := procLoadImage.Call(instance(), 1, 1, size, size, 0) // icon resource 1, IMAGE_ICON
		return h
	}
	cursor, _, _ := procLoadCursor.Call(0, 32512) // IDC_ARROW
	wc := wndClassEx{wndProc: wndProcCB, instance: instance(), icon: icon(11), cursor: cursor,
		background: colorBtnFace + 1, className: utf16(className), iconSm: icon(49)} // SM_CXICON, SM_CXSMICON
	wc.size = uint32(unsafe.Sizeof(wc))
	if r, _, err := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return fmt.Errorf("RegisterClassEx: %v", err)
	}
	return nil
}

func systemDPI() uint32 {
	if procGetDpiForSystem.Find() != nil {
		return 96
	}
	d, _, _ := procGetDpiForSystem.Call()
	if d == 0 {
		return 96
	}
	return uint32(d)
}

// New makes a hidden window with a client area of w×h (96-dpi units), centred on its monitor.
// menu is a menu bar from NewMenu, or 0.
func New(title string, w, h int, menu uintptr) (*Window, error) {
	classOnce.Do(func() { classErr = register() })
	if classErr != nil {
		return nil, classErr
	}
	win := &Window{W: w, H: h, dpi: systemDPI(), style: wsCaption | wsSysMenu | wsMinimizeBox | wsClipChildren,
		exStyle: wsExControlPar, menu: menu}
	win.banner, _, _ = procCreateSolidBrush.Call(BannerColor)
	win.makeFonts() // first: the text size scales the layout
	ow, oh := win.outer()
	creating = win
	hwnd, _, err := procCreateWindowEx.Call(uintptr(win.exStyle), uintptr(unsafe.Pointer(utf16(className))),
		uintptr(unsafe.Pointer(utf16(title))), uintptr(win.style), cwUseDefault, cwUseDefault, uintptr(ow), uintptr(oh),
		0, menu, instance(), 0)
	creating = nil
	if hwnd == 0 {
		return nil, fmt.Errorf("CreateWindowEx: %v", err)
	}
	win.HWND = hwnd
	windows[hwnd] = win
	if d, _, _ := procGetDpiForWindow.Call(hwnd); d != 0 && uint32(d) != win.dpi {
		win.dpi = uint32(d)
		win.makeFonts()
		ow, oh = win.outer()
		procSetWindowPos.Call(hwnd, 0, 0, 0, uintptr(ow), uintptr(oh), swpNoZOrder|swpNoActivate|0x0002) // SWP_NOMOVE
	}
	win.center()
	return win, nil
}

// outer is the window size (frame included) for the client size at the current DPI.
func (w *Window) outer() (int32, int32) {
	r := rect{0, 0, w.Scale(w.W), w.Scale(w.H)}
	hasMenu := uintptr(0)
	if w.menu != 0 {
		hasMenu = 1
	}
	procAdjustWindowRectExForDpi.Call(uintptr(unsafe.Pointer(&r)), uintptr(w.style), hasMenu, uintptr(w.exStyle), uintptr(w.dpi))
	return r.right - r.left, r.bottom - r.top
}

func (w *Window) center() {
	var r rect
	procGetWindowRect.Call(w.HWND, uintptr(unsafe.Pointer(&r)))
	mon, _, _ := procMonitorFromWindow.Call(w.HWND, 2) // MONITOR_DEFAULTTONEAREST
	mi := monitorInfo{size: uint32(unsafe.Sizeof(monitorInfo{}))}
	if ok, _, _ := procGetMonitorInfo.Call(mon, uintptr(unsafe.Pointer(&mi))); ok == 0 {
		return
	}
	x := mi.work.left + (mi.work.right-mi.work.left-(r.right-r.left))/2
	y := mi.work.top + (mi.work.bottom-mi.work.top-(r.bottom-r.top))/2
	procSetWindowPos.Call(w.HWND, 0, uintptr(x), uintptr(y), 0, 0, swpNoZOrder|swpNoActivate|0x0001) // SWP_NOSIZE
}

// DPI is the window's current DPI (96 = 100 %).
func (w *Window) DPI() uint32 { return w.dpi }

// Scale turns layout units into pixels: 96 dpi and 100 % text is one pixel each; the layout grows with
// both the DPI and the user's text size, the way dialog units do.
func (w *Window) Scale(v int) int32 {
	t := w.text
	if t == 0 {
		t = 1
	}
	return int32(float64(v)*float64(w.dpi)/96*t + 0.5)
}

// TextScale is the user's text size (1 = 100 %).
func (w *Window) TextScale() float64 { return w.text }

func (w *Window) makeFonts() {
	old := w.fonts
	ncm := nonClientMetrics{}
	ncm.size = uint32(unsafe.Sizeof(ncm))
	procSystemParametersInfoDpi.Call(0x29, uintptr(ncm.size), uintptr(unsafe.Pointer(&ncm)), 0, uintptr(w.dpi)) // SPI_GETNONCLIENTMETRICS
	lf := ncm.messageFont
	if lf.height == 0 {
		lf.height = -int32(12 * w.dpi / 96)
		copy(lf.faceName[:], syscall.StringToUTF16("Segoe UI"))
	}
	h := float64(lf.height)
	if h < 0 {
		h = -h
	} else {
		h = h * 3 / 4 // a cell height, not a character height
	}
	w.text = min(max(h/(12*float64(w.dpi)/96), 1), 3) // Windows' own message font is 12 px at 96 dpi
	for f := Font(0); f < fontCount; f++ {
		l := lf
		switch f {
		case Bold:
			l.weight = 700
		case Title:
			l.weight = 700
			l.height = l.height * 3 / 2
		}
		w.fonts[f], _, _ = procCreateFontIndirect.Call(uintptr(unsafe.Pointer(&l)))
	}
	for _, c := range w.controls {
		send(c.HWND, wmSetFont, w.fonts[c.Font], 1)
	}
	for _, h := range old {
		if h != 0 {
			procDeleteObject.Call(h)
		}
	}
}

// FontFace is the face name of the window's fonts.
func (w *Window) FontFace() string {
	ncm := nonClientMetrics{}
	ncm.size = uint32(unsafe.Sizeof(ncm))
	procSystemParametersInfoDpi.Call(0x29, uintptr(ncm.size), uintptr(unsafe.Pointer(&ncm)), 0, uintptr(w.dpi))
	return syscall.UTF16ToString(ncm.messageFont.faceName[:])
}

// Show shows the window and brings it to the front.
func (w *Window) Show() {
	procShowWindow.Call(w.HWND, swShow)
	procSetForegroundWindow.Call(w.HWND)
}

// Close destroys the window; Run then returns.
func (w *Window) Close() {
	if !w.closed {
		procDestroyWindow.Call(w.HWND)
	}
}

// Closed reports whether the window is gone.
func (w *Window) Closed() bool { return w.closed }

// SetTitle changes the title bar.
func (w *Window) SetTitle(s string) {
	procSetWindowText.Call(w.HWND, uintptr(unsafe.Pointer(utf16(s))))
}

// Invalidate repaints the window.
func (w *Window) Invalidate() { procInvalidateRect.Call(w.HWND, 0, 1) }

// Wake asks for OnWake to run on the UI thread. Safe from any goroutine; wake-ups not yet handled
// are merged into one, so a fast worker cannot flood the message queue.
func (w *Window) Wake() {
	if w.waking.CompareAndSwap(false, true) {
		procPostMessage.Call(w.HWND, wmApp+1, 0, 0)
	}
}

// SetDefault makes button id the default (Enter presses it).
func (w *Window) SetDefault(id int) {
	w.defID = id
	for _, c := range w.controls {
		if c.Class == "BUTTON" && c.style&0xF <= bsDefPushButton {
			style := uintptr(bsPushButton)
			if c.ID == id {
				style = bsDefPushButton
			}
			send(c.HWND, bmSetStyle, style, 1)
		}
	}
}

// Accelerators sets keyboard shortcuts: key is a virtual-key code, ctrl adds Ctrl.
func (w *Window) Accelerators(keys []Accel) {
	if w.accel != 0 {
		procDestroyAcceleratorTable.Call(w.accel)
	}
	table := make([]accel, len(keys))
	for i, k := range keys {
		table[i] = accel{virt: 1, key: k.Key, cmd: uint16(k.ID)} // FVIRTKEY
		if k.Ctrl {
			table[i].virt |= 8 // FCONTROL
		}
	}
	if len(table) > 0 {
		w.accel, _, _ = procCreateAcceleratorTable.Call(uintptr(unsafe.Pointer(&table[0])), uintptr(len(table)))
	}
}

// Accel is one keyboard shortcut for a command id.
type Accel struct {
	Key  uint16
	Ctrl bool
	ID   int
}

// Run handles messages until the window is closed.
func (w *Window) Run() {
	var m msg
	for !w.closed {
		r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		w.dispatch(&m)
	}
}

// Pump handles the messages waiting now and returns (for -smoke walks, which drive the window).
func (w *Window) Pump() {
	var m msg
	for {
		r, _, _ := procPeekMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, pmRemove)
		if r == 0 || m.message == 0x0012 { // WM_QUIT
			return
		}
		w.dispatch(&m)
	}
}

func (w *Window) dispatch(m *msg) {
	if w.accel != 0 && !w.closed {
		if r, _, _ := procTranslateAccelerator.Call(w.HWND, w.accel, uintptr(unsafe.Pointer(m))); r != 0 {
			return
		}
	}
	if !w.closed {
		if r, _, _ := procIsDialogMessage.Call(w.HWND, uintptr(unsafe.Pointer(m))); r != 0 {
			return
		}
	}
	procTranslateMessage.Call(uintptr(unsafe.Pointer(m)))
	procDispatchMessage.Call(uintptr(unsafe.Pointer(m)))
}

func wndProc(hwnd, m, wparam, lparam uintptr) (result uintptr) {
	w := windows[hwnd]
	if w == nil && creating != nil {
		w = creating
		w.HWND = hwnd
		windows[hwnd] = w
	}
	if w == nil {
		r, _, _ := procDefWindowProc.Call(hwnd, m, wparam, lparam)
		return r
	}
	defer func() {
		if p := recover(); p != nil {
			if w.Panic == nil {
				w.Panic = fmt.Errorf("%v\n%s", p, debug.Stack())
				fmt.Fprintln(os.Stderr, "panic in window handler:", w.Panic)
			}
			result = 0
		}
	}()
	switch uint32(m) {
	case wmCommand:
		if w.OnCommand != nil {
			w.OnCommand(loword(wparam), hiword(wparam))
		}
		return 0
	case wmClose:
		if w.OnClose != nil && !w.OnClose() {
			return 0
		}
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		w.closed = true
		for i, h := range w.fonts {
			procDeleteObject.Call(h)
			w.fonts[i] = 0
		}
		procDeleteObject.Call(w.banner)
		if w.accel != 0 {
			procDestroyAcceleratorTable.Call(w.accel)
		}
		delete(windows, hwnd)
		procPostQuitMessage.Call(0)
		return 0
	case wmPaint:
		var ps paintStruct
		dc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		if w.OnPaint != nil {
			w.OnPaint(&Painter{w: w, dc: dc})
		}
		procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	case wmCtlColorStat, wmCtlColorBtn:
		look := Face
		if c := w.control(lparam); c != nil {
			look = c.Look
		}
		return w.colors(wparam, look)
	case wmDpiChanged:
		w.dpi = uint32(hiword(wparam))
		var suggested rect
		copyMemory(unsafe.Pointer(&suggested), lparam, unsafe.Sizeof(suggested))
		ow, oh := w.outer()
		w.makeFonts()
		w.layout()
		procSetWindowPos.Call(hwnd, 0, uintptr(suggested.left), uintptr(suggested.top), uintptr(ow), uintptr(oh), swpNoZOrder|swpNoActivate)
		if w.OnDPI != nil {
			w.OnDPI()
		}
		w.Invalidate()
		return 0
	case dmGetDefID:
		if w.defID != 0 {
			return uintptr(w.defID) | dcHasDefID<<16
		}
	case wmApp + 1:
		w.waking.Store(false)
		if w.OnWake != nil {
			w.OnWake()
		}
		return 0
	}
	r, _, _ := procDefWindowProc.Call(hwnd, m, wparam, lparam)
	return r
}

func (w *Window) colors(dc uintptr, look Look) uintptr {
	switch look {
	case Paper:
		c, _, _ := procGetSysColor.Call(colorWindow)
		t, _, _ := procGetSysColor.Call(colorWindowText)
		procSetBkColor.Call(dc, c)
		procSetTextColor.Call(dc, t)
		b, _, _ := procGetSysColorBrush.Call(colorWindow)
		return b
	case Banner:
		procSetBkColor.Call(dc, BannerColor)
		procSetTextColor.Call(dc, 0x00FFFFFF)
		return w.banner
	}
	c, _, _ := procGetSysColor.Call(colorBtnFace)
	t, _, _ := procGetSysColor.Call(18) // COLOR_BTNTEXT
	procSetBkColor.Call(dc, c)
	procSetTextColor.Call(dc, t)
	b, _, _ := procGetSysColorBrush.Call(colorBtnFace)
	return b
}

var procMoveMemory = kernel32.NewProc("RtlMoveMemory")

// copyMemory copies n bytes from an address Windows handed us (vet-clean: no uintptr-to-pointer).
func copyMemory(dst unsafe.Pointer, src uintptr, n uintptr) {
	procMoveMemory.Call(uintptr(dst), src, n)
}

func (w *Window) control(hwnd uintptr) *Control {
	for _, c := range w.controls {
		if c.HWND == hwnd {
			return c
		}
	}
	return nil
}

// subclassProc makes a read-only multi-line edit let go of Tab, Enter and Esc, as in a dialog.
func subclassProc(hwnd, m, wparam, lparam, id, ref uintptr) uintptr {
	r, _, _ := procDefSubclassProc.Call(hwnd, m, wparam, lparam)
	if uint32(m) == wmGetDlgCode {
		r &^= dlgcWantAllKeys | dlgcHasSetSel
	}
	return r
}

func (w *Window) layout() {
	for _, c := range w.controls {
		c.place()
	}
}

func (c *Control) place() {
	w := c.w
	procMoveWindow.Call(c.HWND, uintptr(w.Scale(c.Rect.X)), uintptr(w.Scale(c.Rect.Y)), uintptr(w.Scale(c.Rect.W)), uintptr(w.Scale(c.Rect.H)), 1)
	send(c.HWND, wmSetFont, w.fonts[c.Font], 1)
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

// SetText sets a control's text; line ends become CRLF, as edit controls need.
func (c *Control) SetText(s string) {
	if c.Class == "EDIT" {
		s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")
	}
	procSetWindowText.Call(c.HWND, uintptr(unsafe.Pointer(utf16(s))))
}

// Text is a control's text.
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

// Controls are the window's controls, in the order they were made (the Tab order).
func (w *Window) Controls() []*Control { return w.controls }

// IconSet reports whether the window has its own icon: the exe's icon resource 1, as the class icon.
func (w *Window) IconSet() bool {
	icon, _, _ := procGetClassLongPtr.Call(w.HWND, ^uintptr(13)) // GCLP_HICON (-14)
	return icon != 0
}

// MessageBox shows a message box owned by the window; it returns the button pressed (IDOK, IDYES ...).
func (w *Window) MessageBox(title, text string, flags uint32) int {
	owner := uintptr(0)
	if w != nil && !w.closed {
		owner = w.HWND
	}
	r, _, _ := procMessageBox.Call(owner, uintptr(unsafe.Pointer(utf16(text))), uintptr(unsafe.Pointer(utf16(title))), uintptr(flags))
	return int(r)
}

// Message box flags and answers.
const (
	MBOK           = 0x00
	MBOKCancel     = 0x01
	MBYesNo        = 0x04
	MBRetryCancel  = 0x05
	MBIconError    = 0x10
	MBIconQuestion = 0x20
	MBIconWarning  = 0x30
	MBIconInfo     = 0x40
	MBDefButton2   = 0x100
	IDYES          = 6
	IDNO           = 7
	IDRETRY        = 4
)

// NewMenu makes a menu bar from items: each top-level item has a title and its entries
// (an entry with ID 0 is a separator).
func NewMenu(menus []Menu) uintptr {
	bar, _, _ := procCreateMenu.Call()
	for _, m := range menus {
		pop, _, _ := procCreatePopupMenu.Call()
		for _, e := range m.Items {
			if e.ID == 0 {
				procAppendMenu.Call(pop, 0x800, 0, 0) // MF_SEPARATOR
				continue
			}
			procAppendMenu.Call(pop, 0, uintptr(e.ID), uintptr(unsafe.Pointer(utf16(e.Text))))
		}
		procAppendMenu.Call(bar, 0x10, pop, uintptr(unsafe.Pointer(utf16(m.Title)))) // MF_POPUP
	}
	return bar
}

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

// ==== stamp v1 | author=kjFriday | hmac-sha256=e2e543897e19e9992646d20c514749a5346f28131d493e5c3eff2b70da993cbe ====
