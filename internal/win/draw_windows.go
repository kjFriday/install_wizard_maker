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
type Painter struct {
	w  *Window
	dc uintptr
}

// Fill paints r (96-dpi units) in a look's colour.
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
func (p *Painter) Pixels(img image.Image, x, y, size int) {
	if img == nil {
		return
	}
	n := max((int(p.w.Scale(size))+8)/16*16, 16) // a multiple of 16 keeps 16-px pixel art on its grid
	bgra := scaleBGRA(img, n)
	bmi := bitmapInfoHeader{size: 40, width: int32(n), height: -int32(n), planes: 1, bitCount: 32}
	mem, _, _ := procCreateCompatibleDC.Call(p.dc)
	defer procDeleteDC.Call(mem)
	var bits *byte
	bmp, _, _ := procCreateDIBSection.Call(p.dc, uintptr(unsafe.Pointer(&bmi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 {
		return
	}
	defer procDeleteObject.Call(bmp)
	procSetDIBits.Call(p.dc, bmp, 0, uintptr(n), uintptr(unsafe.Pointer(&bgra[0])), uintptr(unsafe.Pointer(&bmi)), 0)
	old, _, _ := procSelectObject.Call(mem, bmp)
	procAlphaBlend.Call(p.dc, uintptr(p.w.Scale(x)), uintptr(p.w.Scale(y)), uintptr(n), uintptr(n),
		mem, 0, 0, uintptr(n), uintptr(n), 0x01FF0000) // AC_SRC_OVER, constant alpha 255, AC_SRC_ALPHA
	procSelectObject.Call(mem, old)
}

// scaleBGRA is img resized to n×n by nearest neighbour, as premultiplied BGRA rows from the top.
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
		row := b[40+4*w*(h-1-y):]
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
func (w *Window) Capture() (*image.RGBA, error) {
	var r rect
	procGetWindowRect.Call(w.HWND, uintptr(unsafe.Pointer(&r)))
	width, height := int(r.right-r.left), int(r.bottom-r.top)
	cw, ch := w.ClientSize()
	if width <= 0 || height <= 0 {
		return nil, errors.New("window has no size")
	}
	var err error
	for _, try := range []struct {
		flags uintptr
		w, h  int
	}{{0, width, height}, {2, width, height}, {1, cw, ch}} { // -, PW_RENDERFULLCONTENT, PW_CLIENTONLY
		var img *image.RGBA
		if img, err = w.print(try.flags, try.w, try.h); err == nil {
			return img, nil
		}
	}
	return nil, err
}

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
func (w *Window) Info() []ControlInfo {
	var out []ControlInfo
	for _, c := range w.controls {
		var r rect
		procGetWindowRect.Call(c.HWND, uintptr(unsafe.Pointer(&r)))
		procMapWindowPoints.Call(0, w.HWND, uintptr(unsafe.Pointer(&r)), 2)
		out = append(out, ControlInfo{Class: c.Class, ID: c.ID, Text: c.TextValue(), X: int(r.left), Y: int(r.top),
			W: int(r.right - r.left), H: int(r.bottom - r.top), Visible: c.Visible(), Enabled: c.Enabled(), Fits: c.fits()})
	}
	return out
}

// ClientSize is the client area in pixels.
func (w *Window) ClientSize() (int, int) {
	var r rect
	procGetClientRect.Call(w.HWND, uintptr(unsafe.Pointer(&r)))
	return int(r.right), int(r.bottom)
}

// fits measures the control's text with its font: labels must fit their box, buttons and check boxes
// their width.
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
	const calc, wordBreak, singleLine, noPrefix = 0x400, 0x10, 0x20, 0x800
	u, _ := syscall.UTF16FromString(text)
	measure := func(flags uintptr, max int32) rect {
		r := rect{0, 0, max, 0}
		procDrawText.Call(dc, uintptr(unsafe.Pointer(&u[0])), ^uintptr(0), uintptr(unsafe.Pointer(&r)), calc|flags)
		return r
	}
	if c.Class == "STATIC" {
		flags := uintptr(wordBreak)
		if c.style&ssNoPrefix != 0 {
			flags |= noPrefix
		}
		if c.style&0xF == ssLeftNoWrap {
			r := measure(singleLine, 0)
			return r.right <= width && r.bottom <= height
		}
		return measure(flags, width).bottom <= height
	}
	r := measure(singleLine, 0)
	pad := w.Scale(10)
	if c.style&0xF == bsAutoCheckBox {
		box, _, _ := procGetSystemMetricsForDpi.Call(71, uintptr(w.dpi)) // SM_CXMENUCHECK
		pad = int32(box) + w.Scale(6)
	}
	return r.right+pad <= width && r.bottom <= height
}
