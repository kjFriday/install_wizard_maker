// ==== ACME Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/win/api_windows.go
// About: Package win is a small Win32 toolkit over syscall (no cgo, no libraries): one window...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Package win is a small Win32 toolkit over syscall (no cgo, no libraries): one window class, native
// controls, Segoe UI fonts, per-monitor DPI scaling, dialog-style keyboard handling, wake-ups from
// worker goroutines, pixel-art drawing and window captures.
//
// Everything here runs on the UI thread (main locks it), except Window.Wake.
package win

import "syscall"

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	comctl32 = syscall.NewLazyDLL("comctl32.dll")
	msimg32  = syscall.NewLazyDLL("msimg32.dll")

	procRegisterClassEx          = user32.NewProc("RegisterClassExW")
	procCreateWindowEx           = user32.NewProc("CreateWindowExW")
	procDefWindowProc            = user32.NewProc("DefWindowProcW")
	procDestroyWindow            = user32.NewProc("DestroyWindow")
	procShowWindow               = user32.NewProc("ShowWindow")
	procGetMessage               = user32.NewProc("GetMessageW")
	procPeekMessage              = user32.NewProc("PeekMessageW")
	procTranslateMessage         = user32.NewProc("TranslateMessage")
	procDispatchMessage          = user32.NewProc("DispatchMessageW")
	procIsDialogMessage          = user32.NewProc("IsDialogMessageW")
	procTranslateAccelerator     = user32.NewProc("TranslateAcceleratorW")
	procCreateAcceleratorTable   = user32.NewProc("CreateAcceleratorTableW")
	procDestroyAcceleratorTable  = user32.NewProc("DestroyAcceleratorTable")
	procPostMessage              = user32.NewProc("PostMessageW")
	procSendMessage              = user32.NewProc("SendMessageW")
	procPostQuitMessage          = user32.NewProc("PostQuitMessage")
	procSetWindowText            = user32.NewProc("SetWindowTextW")
	procGetWindowText            = user32.NewProc("GetWindowTextW")
	procGetWindowTextLength      = user32.NewProc("GetWindowTextLengthW")
	procEnableWindow             = user32.NewProc("EnableWindow")
	procIsWindowEnabled          = user32.NewProc("IsWindowEnabled")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procSetFocus                 = user32.NewProc("SetFocus")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procMoveWindow               = user32.NewProc("MoveWindow")
	procSetWindowPos             = user32.NewProc("SetWindowPos")
	procGetDpiForWindow          = user32.NewProc("GetDpiForWindow")
	procGetDpiForSystem          = user32.NewProc("GetDpiForSystem")
	procAdjustWindowRectExForDpi = user32.NewProc("AdjustWindowRectExForDpi")
	procSystemParametersInfoDpi  = user32.NewProc("SystemParametersInfoForDpi")
	procGetSystemMetricsForDpi   = user32.NewProc("GetSystemMetricsForDpi")
	procLoadImage                = user32.NewProc("LoadImageW")
	procLoadCursor               = user32.NewProc("LoadCursorW")
	procGetClientRect            = user32.NewProc("GetClientRect")
	procGetWindowRect            = user32.NewProc("GetWindowRect")
	procMapWindowPoints          = user32.NewProc("MapWindowPoints")
	procBeginPaint               = user32.NewProc("BeginPaint")
	procEndPaint                 = user32.NewProc("EndPaint")
	procFillRect                 = user32.NewProc("FillRect")
	procGetSysColorBrush         = user32.NewProc("GetSysColorBrush")
	procGetSysColor              = user32.NewProc("GetSysColor")
	procInvalidateRect           = user32.NewProc("InvalidateRect")
	procGetDC                    = user32.NewProc("GetDC")
	procReleaseDC                = user32.NewProc("ReleaseDC")
	procDrawText                 = user32.NewProc("DrawTextW")
	procPrintWindow              = user32.NewProc("PrintWindow")
	procMessageBox               = user32.NewProc("MessageBoxW")
	procGetClassName             = user32.NewProc("GetClassNameW")
	procGetClassLongPtr          = user32.NewProc("GetClassLongPtrW")
	procMonitorFromWindow        = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfo           = user32.NewProc("GetMonitorInfoW")
	procCreateMenu               = user32.NewProc("CreateMenu")
	procCreatePopupMenu          = user32.NewProc("CreatePopupMenu")
	procAppendMenu               = user32.NewProc("AppendMenuW")
	procSetMenu                  = user32.NewProc("SetMenu")
	procCreateFontIndirect       = gdi32.NewProc("CreateFontIndirectW")
	procDeleteObject             = gdi32.NewProc("DeleteObject")
	procSelectObject             = gdi32.NewProc("SelectObject")
	procSetBkColor               = gdi32.NewProc("SetBkColor")
	procSetTextColor             = gdi32.NewProc("SetTextColor")
	procSetBkMode                = gdi32.NewProc("SetBkMode")
	procCreateSolidBrush         = gdi32.NewProc("CreateSolidBrush")
	procCreateCompatibleDC       = gdi32.NewProc("CreateCompatibleDC")
	procDeleteDC                 = gdi32.NewProc("DeleteDC")
	procCreateDIBSection         = gdi32.NewProc("CreateDIBSection")
	procSetDIBits                = gdi32.NewProc("SetDIBits")
	procGetDIBits                = gdi32.NewProc("GetDIBits")
	procCreateCompatibleBitmap   = gdi32.NewProc("CreateCompatibleBitmap")
	procAlphaBlend               = msimg32.NewProc("AlphaBlend")
	procGetModuleHandle          = kernel32.NewProc("GetModuleHandleW")
	procInitCommonControlsEx     = comctl32.NewProc("InitCommonControlsEx")
	procSetWindowSubclass        = comctl32.NewProc("SetWindowSubclass")
	procDefSubclassProc          = comctl32.NewProc("DefSubclassProc")
)

// Window styles and messages used here.
const (
	wsChild         = 0x40000000
	wsVisible       = 0x10000000
	wsCaption       = 0x00C00000
	wsSysMenu       = 0x00080000
	wsMinimizeBox   = 0x00020000
	wsClipChildren  = 0x02000000
	wsTabStop       = 0x00010000
	wsGroup         = 0x00020000
	wsVScroll       = 0x00200000
	wsHScroll       = 0x00100000
	wsExControlPar  = 0x00010000
	wsExClientEdge  = 0x00000200
	cwUseDefault    = 0x80000000
	swHide          = 0
	swShow          = 5
	swpNoZOrder     = 0x0004
	swpNoActivate   = 0x0010
	wmDestroy       = 0x0002
	wmSize          = 0x0005
	wmPaint         = 0x000F
	wmClose         = 0x0010
	wmSetFont       = 0x0030
	wmGetDlgCode    = 0x0087
	wmKeyDown       = 0x0100
	wmCommand       = 0x0111
	wmCtlColorEdit  = 0x0133
	wmCtlColorBtn   = 0x0135
	wmCtlColorStat  = 0x0138
	wmDpiChanged    = 0x02E0
	wmUser          = 0x0400
	wmApp           = 0x8000
	dmGetDefID      = wmUser
	dcHasDefID      = 0x534B
	dlgcWantAllKeys = 0x0004
	dlgcHasSetSel   = 0x0008
	pmRemove        = 0x0001
	colorWindow     = 5
	colorBtnFace    = 15
	colorWindowText = 8
)

// Control styles.
const (
	ssLeft          = 0x0000
	ssNoPrefix      = 0x0080
	ssEtchedHorz    = 0x0010
	ssLeftNoWrap    = 0x000C
	ssEndEllipsis   = 0x4000
	ssPathEllipsis  = 0x8000
	bsPushButton    = 0x0000
	bsDefPushButton = 0x0001
	bsAutoCheckBox  = 0x0003
	esMultiline     = 0x0004
	esAutoVScroll   = 0x0040
	esAutoHScroll   = 0x0080
	esReadOnly      = 0x0800
	lbsNotify       = 0x0001
	lbsNoIntegral   = 0x0100
	cbsDropDown     = 0x0002
	cbsAutoHScroll  = 0x0040
)

// Control messages.
const (
	bmGetCheck     = 0x00F0
	bmSetCheck     = 0x00F1
	bmSetStyle     = 0x00F4
	bmClick        = 0x00F5
	emSetLimitText = 0x00C5
	emSetSel       = 0x00B1
	pbmSetPos      = wmUser + 2
	pbmSetRange32  = wmUser + 6
	lbAddString    = 0x0180
	lbDeleteString = 0x0182
	lbResetContent = 0x0184
	lbSetCurSel    = 0x0186
	lbGetCurSel    = 0x0188
	lbGetCount     = 0x018B
	cbAddString    = 0x0143
	cbResetContent = 0x014B
)

// Notification codes for OnCommand.
const (
	BNClicked     = 0
	ENChange      = 0x0300
	LBNSelChange  = 1
	CBNEditChange = 5
	CBNSelChange  = 1
)

// Dialog-style command ids: Enter and Esc send these.
const (
	IDOK     = 1
	IDCANCEL = 2
)

type point struct{ x, y int32 }

type rect struct{ left, top, right, bottom int32 }

type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
	private uint32
}

type wndClassEx struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   uintptr
	icon       uintptr
	cursor     uintptr
	background uintptr
	menuName   *uint16
	className  *uint16
	iconSm     uintptr
}

type logFont struct {
	height, width, escapement, orientation, weight int32
	italic, underline, strikeOut, charSet          byte
	outPrecision, clipPrecision, quality, pitch    byte
	faceName                                       [32]uint16
}

type nonClientMetrics struct {
	size                                                                uint32
	borderWidth, scrollWidth, scrollHeight, captionWidth, captionHeight int32
	captionFont                                                         logFont
	smCaptionWidth, smCaptionHeight                                     int32
	smCaptionFont                                                       logFont
	menuWidth, menuHeight                                               int32
	menuFont, statusFont, messageFont                                   logFont
	paddedBorderWidth                                                   int32
}

type paintStruct struct {
	hdc         uintptr
	erase       int32
	paint       rect
	restore     int32
	incUpdate   int32
	rgbReserved [32]byte
}

type monitorInfo struct {
	size    uint32
	monitor rect
	work    rect
	flags   uint32
}

type bitmapInfoHeader struct {
	size          uint32
	width, height int32
	planes        uint16
	bitCount      uint16
	compression   uint32
	sizeImage     uint32
	xPels, yPels  int32
	clrUsed       uint32
	clrImportant  uint32
}

type accel struct {
	virt byte
	_    byte
	key  uint16
	cmd  uint16
}

func utf16(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		p, _ = syscall.UTF16PtrFromString("?")
	}
	return p
}

//go:uintptrescapes
func send(h uintptr, m uint32, w, l uintptr) uintptr {
	r, _, _ := procSendMessage.Call(h, uintptr(m), w, l)
	return r
}

func instance() uintptr {
	h, _, _ := procGetModuleHandle.Call(0)
	return h
}

func loword(v uintptr) int { return int(uint16(v)) }
func hiword(v uintptr) int { return int(uint16(v >> 16)) }

// ==== stamp v1 | author=kjFriday | hmac-sha256=2df97e5d13ece29083bc659f8c310d79dd126352937e43ce6016fe1c20d79088 ====
