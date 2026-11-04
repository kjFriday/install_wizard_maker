// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/win/window_windows.go
// About: The top-level window: the toolkit's layout units (Rect, Font, Look), the window class...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// The top-level window: the toolkit's layout units (Rect, Font, Look), the window class, New (sized for
// the monitor's DPI and the user's text size), fonts, the message loop and wndProc, and message boxes.
// Controls are in control_windows.go, menus and keyboard shortcuts in menu_windows.go.

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

// BannerColor is the side banner's colour (COLORREF, 0x00BBGGRR): a dark Generic Company blue.
const BannerColor = 0x005B3923

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

const className = "GenericCompanyWizardWindow"

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
		procSetWindowPos.Call(hwnd, 0, 0, 0, uintptr(ow), uintptr(oh), swpNoZOrder|swpNoActivate|swpNoMove)
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
		procPostMessage.Call(w.HWND, wmWake, 0, 0)
	}
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
		if r == 0 || m.message == wmQuit {
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
	case wmWake:
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
	t, _, _ := procGetSysColor.Call(colorBtnText)
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

// IconSet reports whether the window has its own icon: the exe's icon resource 1, as the class icon.
func (w *Window) IconSet() bool {
	icon, _, _ := procGetClassLongPtr.Call(w.HWND, gclpHIcon)
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

// ==== stamp v1 | author=kjFriday | hmac-sha256=b5f2a3e362c61dda39b59b9ba9f8153045c345027d7971bd62a3b3fc4ea97247 ====
