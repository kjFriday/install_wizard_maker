// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/win/window_windows.go
// About: The top-level window: layout units, the window class, fonts and the message loop.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// The top-level window: layout units, the window class, fonts and the message loop.
//
// This file holds the toolkit's layout units (Rect, Font, Look), the one window class, New (sized for
// the monitor's DPI and the user's text size), the three fonts, the message loop and wndProc, and
// message boxes. Controls are in control_windows.go, menus and keyboard shortcuts in menu_windows.go,
// painting and captures in draw_windows.go, the Win32 procs and constants in api_windows.go.
//
// The maker's window (internal/maker) and the wizard's frame (internal/wizard) use it like this:
//  1. New registers the class once, then creates a hidden window whose client area is W×H layout
//     units at the monitor's DPI and the user's text size, centred on the monitor's work area.
//  2. The caller adds controls (control_windows.go) and sets the On* handlers, then calls Show.
//  3. Run (or Pump, for -smoke walks) takes messages from the queue; dispatch lets accelerators and
//     IsDialogMessage (Tab, Enter, Esc, mnemonics) have them first; wndProc handles the rest.
//  4. Closing destroys the window: wndProc frees its fonts, brush and accelerators and Run returns.
//
// Threading: all of this runs on the thread that called New (main locks it), except Window.Wake.
// The package globals windows and creating are touched only there.
//
// Start reading at New, then wndProc.

package win

import (
	"fmt"
	"os"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// Rect is a rectangle in 96-dpi units; the window scales it to its monitor's DPI.
type Rect struct{ X, Y, W, H int }

// Font is one of a window's three fonts, all from the system's message font (Segoe UI).
type Font int

// The fonts, in the order Window.fonts holds them.
const (
	Normal    Font = iota // the message font as the user set it
	Bold                  // the message font, bold
	Title                 // bold and half again as large
	fontCount             // how many fonts there are; not a font
)

// Look is what a control (or a painted area) sits on.
type Look int

// The looks; wndProc answers WM_CTLCOLORSTATIC and WM_CTLCOLORBTN with their colours (colors).
const (
	Face   Look = iota // the dialog colour
	Paper              // white: page headers, welcome and finish pages
	Banner             // the welcome and finish pages' side banner: dark, white text
)

// BannerColor is the side banner's colour (COLORREF, 0x00BBGGRR): a dark Generic Company blue.
const BannerColor = 0x005B3923

// Window is a fixed-size top-level window holding controls.
//
// Make one with New. Its fields other than waking belong to the UI thread; the On* handlers run
// there too, from wndProc. A panic in a handler is recovered into Panic, so the window keeps going.
type Window struct {
	HWND     uintptr
	W, H     int // client size in 96-dpi units
	dpi      uint32
	text     float64 // the user's text size (Settings > Make text bigger): 1 = 100 %
	style    uint32  // WS_* window styles, kept for AdjustWindowRectExForDpi
	exStyle  uint32  // WS_EX_* styles, likewise
	menu     uintptr // the menu bar, or 0
	controls []*Control
	fonts    [fontCount]uintptr // HFONTs, indexed by Font
	banner   uintptr            // the HBRUSH of BannerColor
	accel    uintptr            // the HACCEL that Accelerators made, or 0
	defID    int                // the default button's id (SetDefault), or 0
	waking   atomic.Bool        // a wake-up is posted and not yet handled
	closed   bool               // WM_DESTROY has run

	OnCommand func(id, code int) // a button, menu item or control notification
	OnClose   func() bool        // the close box or Alt+F4; false keeps the window open
	OnPaint   func(p *Painter)   // draw the background areas
	OnWake    func()             // after Wake, on the UI thread
	OnDPI     func()             // after the window moved to a monitor with another DPI
	Panic     error              // the first panic a handler raised (recovered, so the window lives on)
}

// Package state, all of it used on the UI thread only.
//
// classOnce and classErr register the window class once per process. windows maps each HWND to its
// Window for wndProc. creating is the Window that New is making: CreateWindowEx sends messages
// (WM_NCCREATE, WM_CREATE ...) before it returns the HWND, and wndProc adopts the handle through it.
// wndProcCB and subclassCB are the Go functions as C callbacks; syscall.NewCallback has a fixed
// process-wide limit, so they are made once here, never per window.
var (
	classOnce  sync.Once
	classErr   error
	windows    = map[uintptr]*Window{}
	creating   *Window
	wndProcCB  = syscall.NewCallback(wndProc)
	subclassCB = syscall.NewCallback(subclassProc)
)

// className is the window class every Window uses, registered by register.
const className = "GenericCompanyWizardWindow"

// register loads the common controls and registers the window class.
//
// Steps:
//  1. InitCommonControlsEx loads the standard and Windows 95 control classes (progress bars and the
//     rest); with the exe's Common Controls 6 manifest they come themed.
//  2. The class icons are the exe's icon resource 1, loaded at the system DPI's icon sizes
//     (SM_CXICON for the big one, SM_CXSMICON for the title bar and taskbar).
//  3. The class gets the arrow cursor, the dialog colour as background (COLOR_BTNFACE+1: Windows
//     wants a system colour index plus one) and wndProc.
//
// The error is RegisterClassEx's, for New to return.
func register() error {
	icc := [2]uint32{8, iccWin95Classes | iccStandardClasses} // INITCOMMONCONTROLSEX: its size, the classes
	procInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))
	dpi := systemDPI()
	icon := func(metric uintptr) uintptr {
		size, _, _ := procGetSystemMetricsForDpi.Call(metric, uintptr(dpi))
		h, _, _ := procLoadImage.Call(instance(), 1, imageIcon, size, size, 0) // icon resource 1
		return h
	}
	cursor, _, _ := procLoadCursor.Call(0, idcArrow)
	wc := wndClassEx{wndProc: wndProcCB, instance: instance(), icon: icon(smCxIcon), cursor: cursor,
		background: colorBtnFace + 1, className: utf16(className), iconSm: icon(smCxSmIcon)}
	wc.size = uint32(unsafe.Sizeof(wc))
	if r, _, err := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return fmt.Errorf("RegisterClassEx: %v", err)
	}
	return nil
}

// systemDPI is the system DPI (the primary monitor's at sign-in), or 96 where GetDpiForSystem is
// missing (before Windows 10 1607) or fails. New starts from it, then switches to the window's own.
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
//
// The window has a caption, system menu and minimise box but no resizing frame. Call it on the thread
// that will run the window's messages; the error is RegisterClassEx's or CreateWindowEx's.
//
// Steps:
//  1. Register the class (once per process).
//  2. Make the fonts at the system DPI first: the user's text size, found from the message font,
//     scales the layout, so the outer size depends on it.
//  3. Create the window at that size; creating lets wndProc adopt the HWND during CreateWindowEx.
//  4. A window that opens on a monitor of another DPI (GetDpiForWindow) remakes its fonts and size.
//  5. Centre it on its monitor's work area.
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
		procSetWindowPos.Call(hwnd, 0, 0, 0, uintptr(ow), uintptr(oh), swpNoZOrder|swpNoActivate|swpNoMove)
	}
	win.center()
	return win, nil
}

// outer is the window size (frame included) for the client size at the current DPI.
//
// AdjustWindowRectExForDpi adds the caption, borders and menu bar as they are drawn at w.dpi; the
// plain AdjustWindowRectEx would use the system DPI's sizes, wrong on a monitor of another DPI.
func (w *Window) outer() (int32, int32) {
	r := rect{0, 0, w.Scale(w.W), w.Scale(w.H)}
	hasMenu := uintptr(0)
	if w.menu != 0 {
		hasMenu = 1
	}
	procAdjustWindowRectExForDpi.Call(uintptr(unsafe.Pointer(&r)), uintptr(w.style), hasMenu, uintptr(w.exStyle), uintptr(w.dpi))
	return r.right - r.left, r.bottom - r.top
}

// center moves the window to the middle of its monitor's work area (the screen less the taskbar).
// It leaves the window where it is when the monitor cannot be asked.
func (w *Window) center() {
	var r rect
	procGetWindowRect.Call(w.HWND, uintptr(unsafe.Pointer(&r)))
	mon, _, _ := procMonitorFromWindow.Call(w.HWND, monitorDefaultToNearest)
	mi := monitorInfo{size: uint32(unsafe.Sizeof(monitorInfo{}))}
	if ok, _, _ := procGetMonitorInfo.Call(mon, uintptr(unsafe.Pointer(&mi))); ok == 0 {
		return
	}
	x := mi.work.left + (mi.work.right-mi.work.left-(r.right-r.left))/2
	y := mi.work.top + (mi.work.bottom-mi.work.top-(r.bottom-r.top))/2
	procSetWindowPos.Call(w.HWND, 0, uintptr(x), uintptr(y), 0, 0, swpNoZOrder|swpNoActivate|swpNoSize)
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

// makeFonts makes the three fonts at the window's DPI and works out the user's text size.
//
// Steps:
//  1. Ask for the non-client metrics at w.dpi (SPI_GETNONCLIENTMETRICS); their message font is what
//     message boxes use, and it grows with Settings > Make text bigger. A zero height (the call
//     failed) falls back to 12 px Segoe UI.
//  2. The text scale is that font's height over 12 px at the DPI, kept to 1..3. A negative lfHeight
//     is the character height; a positive one is the cell height, about 4/3 of it.
//  3. Make Normal, Bold and Title (bold, 3/2 the height) from it.
//  4. Give every existing control its new font, then delete the old ones, so no control is ever left
//     holding a deleted font.
func (w *Window) makeFonts() {
	old := w.fonts
	ncm := nonClientMetrics{}
	ncm.size = uint32(unsafe.Sizeof(ncm))
	procSystemParametersInfoDpi.Call(spiGetNonClientMetrics, uintptr(ncm.size), uintptr(unsafe.Pointer(&ncm)), 0, uintptr(w.dpi))
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
			l.weight = fwBold
		case Title:
			l.weight = fwBold
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
//
// It asks Windows again rather than reading the fonts, so it reports the system's message font even
// when makeFonts fell back to Segoe UI.
func (w *Window) FontFace() string {
	ncm := nonClientMetrics{}
	ncm.size = uint32(unsafe.Sizeof(ncm))
	procSystemParametersInfoDpi.Call(spiGetNonClientMetrics, uintptr(ncm.size), uintptr(unsafe.Pointer(&ncm)), 0, uintptr(w.dpi))
	return syscall.UTF16ToString(ncm.messageFont.faceName[:])
}

// Show shows the window and brings it to the front.
func (w *Window) Show() {
	procShowWindow.Call(w.HWND, swShow)
	procSetForegroundWindow.Call(w.HWND)
}

// Close destroys the window; Run then returns.
//
// Unlike the close box it does not ask OnClose. Calling it again is harmless.
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
//
// The merging: the first caller flips waking from false to true and posts WM_APP+1; later callers
// see true and post nothing. wndProc clears the flag before calling OnWake, so a Wake that comes
// while OnWake runs posts again and its news is not lost.
func (w *Window) Wake() {
	if w.waking.CompareAndSwap(false, true) {
		procPostMessage.Call(w.HWND, wmWake, 0, 0)
	}
}

// Run handles messages until the window is closed.
//
// It takes every message of the thread (hwnd 0), the controls' included, and stops on WM_QUIT, which
// wndProc posts when the window is destroyed.
func (w *Window) Run() {
	var m msg
	for !w.closed {
		r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 { // 0 is WM_QUIT, -1 an error
			return
		}
		w.dispatch(&m)
	}
}

// Pump handles the messages waiting now and returns (for -smoke walks, which drive the window).
//
// It also stops at WM_QUIT, which it takes off the queue and drops.
func (w *Window) Pump() {
	var m msg
	for {
		r, _, _ := procPeekMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, pmRemove)
		if r == 0 || m.message == wmQuit {
			return
		}
		w.dispatch(&m)
	}
}

// dispatch hands one message to whoever should have it, in the order Windows dialogs use.
//
// Steps:
//  1. The accelerator table (menu shortcuts such as F5) turns a key into WM_COMMAND.
//  2. IsDialogMessage gives the window dialog keys: Tab and Shift+Tab move the focus, arrows move
//     within a group, Alt+letter presses a mnemonic, Enter presses the default button (it asks
//     DM_GETDEFID, see wndProc) and Esc sends IDCANCEL.
//  3. Anything else is translated (WM_KEYDOWN to WM_CHAR) and sent to its window procedure.
//
// Both of the first two are skipped once the window is destroyed: its handle is no longer valid.
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

// wndProc is the window procedure of every Window: Windows calls it with each message sent or
// dispatched to the window.
//
// It finds the Window by handle, adopting the one New is creating for the messages CreateWindowEx
// sends before it returns. A panic in any handler is recovered: the first is kept in w.Panic and
// printed to stderr, and the message returns 0. Messages it does not handle go to DefWindowProc.
//
// The messages it handles:
//   - WM_COMMAND: OnCommand(id, notification code), from buttons, menus and controls.
//   - WM_CLOSE: the close box or Alt+F4; OnClose may refuse.
//   - WM_DESTROY: frees the fonts, banner brush and accelerators and posts WM_QUIT to end Run.
//   - WM_PAINT: OnPaint draws the background areas between BeginPaint and EndPaint.
//   - WM_CTLCOLORSTATIC and WM_CTLCOLORBTN: a label's or button's colours, after its Look.
//   - WM_DPI_CHANGED: the window moved to a monitor of another DPI; refont, relayout, resize.
//   - DM_GETDEFID: IsDialogMessage asks which button Enter presses.
//   - WM_APP+1 (wmWake): runs OnWake.
func wndProc(hwnd, m, wparam, lparam uintptr) (result uintptr) {
	w := windows[hwnd]
	if w == nil && creating != nil { // a message sent from inside CreateWindowEx, before New knows the HWND
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
		w.dpi = uint32(hiword(wparam)) // the new DPI is in both halves (x and y); they are always equal
		// lparam points at the RECT Windows suggests; only its corner is used, since the size is
		// worked out here from the layout (the text scale is a ratio, so it holds across DPIs)
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
		if w.defID != 0 { // the id in the low word, DC_HASDEFID in the high word, as a dialog answers
			return uintptr(w.defID) | dcHasDefID<<16
		}
		// no default button: DefWindowProc answers that there is none
	case wmWake:
		w.waking.Store(false) // first, so a Wake during OnWake is not merged into this one
		if w.OnWake != nil {
			w.OnWake()
		}
		return 0
	}
	r, _, _ := procDefWindowProc.Call(hwnd, m, wparam, lparam)
	return r
}

// colors sets a control's text and background colours on its device context dc and returns the
// brush that paints the rest of it, as WM_CTLCOLORSTATIC and WM_CTLCOLORBTN expect.
//
// Paper uses the system window colours, Banner white on BannerColor, and Face the dialog colours.
// The system brushes belong to Windows and are never deleted; the banner brush is the window's own.
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
	t, _, _ := procGetSysColor.Call(colorBtnText)
	procSetBkColor.Call(dc, c)
	procSetTextColor.Call(dc, t)
	b, _, _ := procGetSysColorBrush.Call(colorBtnFace)
	return b
}

// procMoveMemory is kernel32's RtlMoveMemory (memmove), used by copyMemory.
var procMoveMemory = kernel32.NewProc("RtlMoveMemory")

// copyMemory copies n bytes from an address Windows handed us (vet-clean: no uintptr-to-pointer).
func copyMemory(dst unsafe.Pointer, src uintptr, n uintptr) {
	procMoveMemory.Call(uintptr(dst), src, n)
}

// IconSet reports whether the window has its own icon: the exe's icon resource 1, as the class icon.
func (w *Window) IconSet() bool {
	icon, _, _ := procGetClassLongPtr.Call(w.HWND, gclpHIcon)
	return icon != 0
}

// MessageBox shows a message box owned by the window; it returns the button pressed (IDOK, IDYES ...).
//
// flags are MB_* values (MBYesNo, MBIconQuestion ...). w may be nil or closed: the box then has no
// owner. It blocks until the user answers, running its own message loop meanwhile.
func (w *Window) MessageBox(title, text string, flags uint32) int {
	owner := uintptr(0)
	if w != nil && !w.closed {
		owner = w.HWND
	}
	r, _, _ := procMessageBox.Call(owner, uintptr(unsafe.Pointer(utf16(text))), uintptr(unsafe.Pointer(utf16(title))), uintptr(flags))
	return int(r)
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=28416960c9521114f4af034d6af32b711dbd227fcc1f9ed9537e2cd921bac667 ====
