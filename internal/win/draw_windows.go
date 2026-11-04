// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/win/draw_windows.go
// About: Painting, icons and captures: what a window draws itself, and how it is seen from outside.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Painting, icons and captures: what a window draws itself, and how it is seen from outside.
//
// Painter is what OnPaint gets: it fills areas in a Look's colour and draws pixel art crisply at
// any DPI. LargestIcon decodes an .ico for drawing (the wizard's banner shows the program's icon).
// Capture, Info and fits serve the -smoke walk (smoke_windows.go): a PNG of the window, and each
// control's place, state and whether its text fits.
//
// GDI in short: drawing goes to a device context (DC). To draw a bitmap, it is selected into a
// memory DC compatible with the target, then copied across (here with AlphaBlend, which mixes by
// per-pixel alpha). A device-independent bitmap (DIB) is a plain array of pixels described by a
// BITMAPINFOHEADER; 32-bit DIBs store each pixel as B, G, R, A bytes. Every DC, bitmap and object
// selected into a DC must be released or deselected, which the defers here do.

package win

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"syscall"
	"unsafe"
)

// Painter draws a window's background during OnPaint.
//
// It is valid only during that call: dc is the device context BeginPaint returned.
type Painter struct {
	w  *Window
	dc uintptr
}

// Fill paints r (96-dpi units) in a look's colour.
//
// Paper is the system window colour, Banner the window's own banner brush, anything else the
// dialog face colour. The system brushes belong to Windows and are not deleted.
func (p *Painter) Fill(r Rect, look Look) {
	rc := rect{p.w.Scale(r.X), p.w.Scale(r.Y), p.w.Scale(r.X + r.W), p.w.Scale(r.Y + r.H)}
	var brush uintptr
	switch look {
	case Paper:
		brush, _, _ = procGetSysColorBrush.Call(colorWindow)
	case Banner:
		brush = p.w.banner
	default:
		brush, _, _ = procGetSysColorBrush.Call(colorBtnFace)
	}
	procFillRect.Call(p.dc, uintptr(unsafe.Pointer(&rc)), brush)
}

// Pixels draws an image at (x, y) (96-dpi units) as size×size (96-dpi units), enlarged or reduced by
// nearest neighbour, so pixel art stays crisp at any DPI.
//
// The drawn size is the scaled size rounded to the nearest multiple of 16 pixels (at least 16), so
// it can differ from size by up to 8 pixels. Transparent pixels show what is under them. A nil
// image, or a bitmap GDI cannot make, draws nothing.
//
// Steps:
//  1. Resize to n×n premultiplied BGRA (scaleBGRA).
//  2. Make an n×n 32-bit DIB section and copy the pixels into it.
//  3. Select it into a memory DC and AlphaBlend it onto the window at (x, y).
func (p *Painter) Pixels(img image.Image, x, y, size int) {
	if img == nil {
		return
	}
	n := max((int(p.w.Scale(size))+8)/16*16, 16) // a multiple of 16 keeps 16-px pixel art on its grid
	bgra := scaleBGRA(img, n)
	// negative height: rows run from the top, as scaleBGRA writes them
	bmi := bitmapInfoHeader{size: 40, width: int32(n), height: -int32(n), planes: 1, bitCount: 32}
	mem, _, _ := procCreateCompatibleDC.Call(p.dc)
	defer procDeleteDC.Call(mem)
	var bits *byte
	bmp, _, _ := procCreateDIBSection.Call(p.dc, uintptr(unsafe.Pointer(&bmi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 {
		return
	}
	defer procDeleteObject.Call(bmp)
	// all n rows from row 0; the last 0 is DIB_RGB_COLORS (plain colour values, no palette)
	procSetDIBits.Call(p.dc, bmp, 0, uintptr(n), uintptr(unsafe.Pointer(&bgra[0])), uintptr(unsafe.Pointer(&bmi)), 0)
	old, _, _ := procSelectObject.Call(mem, bmp)
	procAlphaBlend.Call(p.dc, uintptr(p.w.Scale(x)), uintptr(p.w.Scale(y)), uintptr(n), uintptr(n),
		mem, 0, 0, uintptr(n), uintptr(n), blendSrcAlpha)
	procSelectObject.Call(mem, old)
}

// scaleBGRA is img resized to n×n by nearest neighbour, as premultiplied BGRA rows from the top.
//
// Nearest neighbour: output pixel (x, y) takes the source pixel at (x*w/n, y*h/n), so each source
// pixel becomes a block of equal pixels and no colours are blended. Premultiplied: each colour
// channel is scaled by alpha/255 beforehand, which AlphaBlend with AC_SRC_ALPHA expects.
func scaleBGRA(img image.Image, n int) []byte {
	b := img.Bounds()
	out := make([]byte, 4*n*n)
	for y := 0; y < n; y++ {
		sy := b.Min.Y + y*b.Dy()/n
		for x := 0; x < n; x++ {
			c := color.NRGBAModel.Convert(img.At(b.Min.X+x*b.Dx()/n, sy)).(color.NRGBA)
			a := uint32(c.A)
			i := 4 * (y*n + x)
			out[i], out[i+1], out[i+2], out[i+3] = byte(uint32(c.B)*a/255), byte(uint32(c.G)*a/255), byte(uint32(c.R)*a/255), c.A
		}
	}
	return out
}

// LargestIcon is the biggest image of an .ico file (a PNG, or a 32-bit BMP), for drawing.
//
// The .ico format: a 6-byte header (reserved, type 1 for icons, image count), then one 16-byte
// entry per image whose bytes 8-11 are the image's length and 12-15 its offset in the file, all
// little-endian. Each image is either a whole PNG file or a BMP without its file header. "Biggest"
// is by decoded width. Images it cannot read (other bit depths, damaged entries) are skipped; nil
// when none is readable.
func LargestIcon(ico []byte) image.Image {
	le := binary.LittleEndian
	if len(ico) < 6 || le.Uint16(ico[2:]) != 1 {
		return nil
	}
	var best image.Image
	bestSize := 0
	for i := 0; i < int(le.Uint16(ico[4:])) && 6+16*(i+1) <= len(ico); i++ {
		e := ico[6+16*i:]
		size, off := int(le.Uint32(e[8:])), int(le.Uint32(e[12:]))
		// 40 bytes is the smallest image worth reading: a BITMAPINFOHEADER alone
		if off+size > len(ico) || size < 40 {
			continue
		}
		data := ico[off : off+size]
		var img image.Image
		if bytes.HasPrefix(data, []byte("\x89PNG")) {
			img, _ = png.Decode(bytes.NewReader(data))
		} else {
			img = decodeIconBMP(data)
		}
		if img != nil && img.Bounds().Dx() > bestSize {
			best, bestSize = img, img.Bounds().Dx()
		}
	}
	return best
}

// decodeIconBMP reads a 32-bit icon bitmap (BITMAPINFOHEADER, then BGRA rows from the bottom).
//
// b must be at least 40 bytes (LargestIcon sees to it). Header offsets: 0 the header size (40),
// 4 the width, 8 the height, 14 the bits per pixel. An icon's header gives twice the image's
// height, because the colour rows are followed by a 1-bit AND mask of the same height; with 32-bit
// pixels the alpha channel says it all, so the mask is ignored. Other bit depths give nil.
func decodeIconBMP(b []byte) image.Image {
	le := binary.LittleEndian
	if le.Uint32(b) != 40 || le.Uint16(b[14:]) != 32 {
		return nil
	}
	w, h := int(int32(le.Uint32(b[4:]))), int(int32(le.Uint32(b[8:])))/2
	if w <= 0 || h <= 0 || 40+4*w*h > len(b) {
		return nil
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		row := b[40+4*w*(h-1-y):] // stored bottom row first
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: row[4*x+2], G: row[4*x+1], B: row[4*x], A: row[4*x+3]})
		}
	}
	return img
}

// Capture renders the window with PrintWindow, so windows on top of it do not matter. It first asks
// the window to paint itself now (never a frame behind, as a copy of the composed window can be); some
// windows will not (one with a menu bar, started by another program), so it then copies the composed
// window, and last just the client area.
//
// The image is the whole window, frame included (only the client area when the last try is the
// one that works), in pixels, opaque. The error is the last try's. The -smoke walk calls it for
// each step.
func (w *Window) Capture() (*image.RGBA, error) {
	var r rect
	procGetWindowRect.Call(w.HWND, uintptr(unsafe.Pointer(&r)))
	width, height := int(r.right-r.left), int(r.bottom-r.top)
	cw, ch := w.ClientSize()
	if width <= 0 || height <= 0 {
		return nil, errors.New("window has no size")
	}
	var err error
	// the tries in order: plain PrintWindow, PW_RENDERFULLCONTENT, then PW_CLIENTONLY at client size
	for _, try := range []struct {
		flags uintptr
		w, h  int
	}{{0, width, height}, {pwRenderFullContent, width, height}, {pwClientOnly, cw, ch}} {
		var img *image.RGBA
		if img, err = w.print(try.flags, try.w, try.h); err == nil {
			return img, nil
		}
	}
	return nil, err
}

// print is one PrintWindow try: the window drawn with flags into a width×height bitmap, read back
// as RGBA.
//
// Steps:
//  1. Make a memory DC and a bitmap compatible with the window's DC, and select the bitmap.
//  2. PrintWindow asks the window to paint into it; deselect the bitmap again.
//  3. GetDIBits copies the pixels out as 32-bit top-down BGRA.
//  4. Swap to RGBA and set alpha to 255, since GDI leaves the alpha byte undefined.
func (w *Window) print(flags uintptr, width, height int) (*image.RGBA, error) {
	dc, _, _ := procGetDC.Call(w.HWND)
	defer procReleaseDC.Call(w.HWND, dc)
	mem, _, _ := procCreateCompatibleDC.Call(dc)
	defer procDeleteDC.Call(mem)
	bmp, _, _ := procCreateCompatibleBitmap.Call(dc, uintptr(width), uintptr(height))
	defer procDeleteObject.Call(bmp)
	old, _, _ := procSelectObject.Call(mem, bmp)
	ok, _, err := procPrintWindow.Call(w.HWND, mem, flags)
	procSelectObject.Call(mem, old)
	if ok == 0 {
		return nil, fmt.Errorf("PrintWindow(%d): %v", flags, err)
	}
	bmi := bitmapInfoHeader{size: 40, width: int32(width), height: -int32(height), planes: 1, bitCount: 32}
	buf := make([]byte, 4*width*height)
	// GetDIBits wants the bitmap not selected into a DC, hence the deselect above
	if n, _, err := procGetDIBits.Call(mem, bmp, 0, uintptr(height), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bmi)), 0); n == 0 {
		return nil, fmt.Errorf("GetDIBits: %v", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for i := 0; i < width*height; i++ {
		img.Pix[4*i], img.Pix[4*i+1], img.Pix[4*i+2], img.Pix[4*i+3] = buf[4*i+2], buf[4*i+1], buf[4*i], 255
	}
	return img, nil
}

// ControlInfo describes a control as it is on screen (for -smoke reports).
//
// The JSON names are read by main_test.go's smoke checks; renaming one breaks those tests.
type ControlInfo struct {
	Class   string `json:"class"`
	ID      int    `json:"id"`
	Text    string `json:"text"`
	X       int    `json:"x"` // client pixels
	Y       int    `json:"y"`
	W       int    `json:"w"`
	H       int    `json:"h"`
	Visible bool   `json:"visible"`
	Enabled bool   `json:"enabled"`
	Fits    bool   `json:"fits"` // the text fits in the control at its font
}

// Info describes every control.
//
// The controls come in the order they were made. Positions are client-area pixels of the window.
func (w *Window) Info() []ControlInfo {
	var out []ControlInfo
	for _, c := range w.controls {
		var r rect
		procGetWindowRect.Call(c.HWND, uintptr(unsafe.Pointer(&r)))
		// screen pixels (hWndFrom 0) to the window's client area; a RECT is two POINTs
		procMapWindowPoints.Call(0, w.HWND, uintptr(unsafe.Pointer(&r)), 2)
		out = append(out, ControlInfo{Class: c.Class, ID: c.ID, Text: c.TextValue(), X: int(r.left), Y: int(r.top),
			W: int(r.right - r.left), H: int(r.bottom - r.top), Visible: c.Visible(), Enabled: c.Enabled(), Fits: c.fits()})
	}
	return out
}

// ClientSize is the client area in pixels: width, then height.
func (w *Window) ClientSize() (int, int) {
	var r rect
	procGetClientRect.Call(w.HWND, uintptr(unsafe.Pointer(&r)))
	return int(r.right), int(r.bottom)
}

// fits measures the control's text with its font: labels must fit their box, buttons and check boxes
// their width.
//
// Empty controls, controls other than STATIC and BUTTON, separators and path labels (which shorten
// themselves) always fit. DrawText with DT_CALCRECT measures without drawing: it sets the rect's
// right and bottom to the size the text needs.
//
// Steps:
//  1. Select the control's font into its DC.
//  2. A wrapping label: wrap the text at the control's width; its height must fit.
//  3. A one-line label: measure one line; width and height must fit.
//  4. A button: one line plus a margin for the button's border (10 units), or for a check box
//     the box's width (SM_CXMENUCHECK) and a 6-unit gap; width and height must fit.
func (c *Control) fits() bool {
	text := c.TextValue()
	if text == "" || (c.Class != "STATIC" && c.Class != "BUTTON") || c.Class == "STATIC" && c.style&(ssEtchedHorz|ssPathEllipsis) != 0 {
		return true
	}
	w := c.w
	dc, _, _ := procGetDC.Call(c.HWND)
	defer procReleaseDC.Call(c.HWND, dc)
	old, _, _ := procSelectObject.Call(dc, w.fonts[c.Font])
	defer procSelectObject.Call(dc, old)
	width, height := int32(w.Scale(c.Rect.W)), int32(w.Scale(c.Rect.H))
	u, _ := syscall.UTF16FromString(text)
	// measure is the text's size with flags in a box max pixels wide (length -1: up to the NUL)
	measure := func(flags uintptr, max int32) rect {
		r := rect{0, 0, max, 0}
		procDrawText.Call(dc, uintptr(unsafe.Pointer(&u[0])), ^uintptr(0), uintptr(unsafe.Pointer(&r)), dtCalcRect|flags)
		return r
	}
	if c.Class == "STATIC" {
		flags := uintptr(dtWordBreak)
		if c.style&ssNoPrefix != 0 {
			flags |= dtNoPrefix
		}
		if c.style&typeMask == ssLeftNoWrap {
			r := measure(dtSingleLine, 0)
			return r.right <= width && r.bottom <= height
		}
		return measure(flags, width).bottom <= height
	}
	r := measure(dtSingleLine, 0)
	pad := w.Scale(10)
	if c.style&typeMask == bsAutoCheckBox {
		box, _, _ := procGetSystemMetricsForDpi.Call(smCxMenuCheck, uintptr(w.dpi))
		pad = int32(box) + w.Scale(6)
	}
	return r.right+pad <= width && r.bottom <= height
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=6c6b3e7ecb09586e6287ca63eec32ca02f5aad5a27849d10431dcb448904c182 ====
