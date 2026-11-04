// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/win/win_windows_test.go
// About: Tests for the toolkit: struct sizes, the icon scaler and a real window's round trip.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Tests for the toolkit: struct sizes, the icon scaler and a real window's round trip.
//
// TestWindowRoundTrip shows a real window on this desktop for a moment, so these tests need an
// interactive Windows session.

package win

import (
	"image"
	"image/color"
	"runtime"
	"testing"
	"time"
	"unsafe"
)

// TestStructSizes pins the Go mirrors of Win32 structs to their 64-bit sizes from the Windows SDK
// headers: a wrong size makes Windows read or write past the struct, or reject it (cbSize checks).
func TestStructSizes(t *testing.T) {
	for name, got := range map[string]uintptr{
		"WNDCLASSEXW": unsafe.Sizeof(wndClassEx{}), "MSG": unsafe.Sizeof(msg{}), "LOGFONTW": unsafe.Sizeof(logFont{}),
		"NONCLIENTMETRICSW": unsafe.Sizeof(nonClientMetrics{}), "PAINTSTRUCT": unsafe.Sizeof(paintStruct{}),
		"MONITORINFO": unsafe.Sizeof(monitorInfo{}), "BITMAPINFOHEADER": unsafe.Sizeof(bitmapInfoHeader{}),
		"ACCEL": unsafe.Sizeof(accel{}), "RECT": unsafe.Sizeof(rect{}),
	} {
		want := map[string]uintptr{"WNDCLASSEXW": 80, "MSG": 48, "LOGFONTW": 92, "NONCLIENTMETRICSW": 504,
			"PAINTSTRUCT": 72, "MONITORINFO": 40, "BITMAPINFOHEADER": 40, "ACCEL": 6, "RECT": 16}[name]
		if got != want {
			t.Errorf("%s is %d bytes, want %d", name, got, want)
		}
	}
}

// TestScaleBGRA checks nearest-neighbour doubling and the byte order and premultiplication: opaque
// red becomes B 0, G 0, R 255, A 255, and half-transparent blue (alpha 128) becomes B 128 with A 128.
func TestScaleBGRA(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	img.SetNRGBA(1, 1, color.NRGBA{B: 255, A: 128})
	out := scaleBGRA(img, 4) // each pixel becomes a 2×2 block
	px := func(x, y int) [4]byte { i := 4 * (y*4 + x); return [4]byte{out[i], out[i+1], out[i+2], out[i+3]} }
	if px(1, 1) != [4]byte{0, 0, 255, 255} || px(3, 3) != [4]byte{128, 0, 0, 128} || px(2, 0) != [4]byte{} {
		t.Fatalf("%v %v %v", px(1, 1), px(3, 3), px(2, 0))
	}
}

// TestWindowRoundTrip drives a real window: a check box click reaches OnCommand, text set is text
// read (a multi-line edit turns \n into \r\n), five Wakes from goroutines merge into one to five
// OnWake calls, Info sees the long label overflow its 16-unit box, and Capture and ClientSize follow
// the DPI and text size.
func TestWindowRoundTrip(t *testing.T) {
	runtime.LockOSThread() // a window belongs to the thread that made it; its messages come only there
	defer runtime.UnlockOSThread()
	w, err := New("win test", 300, 200, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	var clicked, woke int
	w.OnCommand = func(id, code int) {
		if id == 7 && code == BNClicked {
			clicked++
		}
	}
	w.OnWake = func() { woke++ }
	label := w.Label("A label long enough to need a second line in this narrow box", Rect{10, 10, 120, 16}, Normal, Paper)
	box := w.Check(7, "&Tick me", Rect{10, 40, 200, 20}, Face)
	edit := w.Edit(8, "start", Rect{10, 70, 200, 23})
	text := w.Text(9, "line one\nline two", Rect{10, 100, 200, 60})
	w.Show()
	w.Pump()
	box.Click()
	if !box.Checked() || clicked != 1 {
		t.Fatalf("click: checked %v, clicked %d", box.Checked(), clicked)
	}
	edit.SetText("typed")
	if edit.TextValue() != "typed" || text.TextValue() != "line one\r\nline two" {
		t.Fatalf("text %q %q", edit.TextValue(), text.TextValue())
	}
	for i := 0; i < 5; i++ {
		go w.Wake()
	}
	time.Sleep(50 * time.Millisecond)
	w.Pump()
	if woke < 1 || woke > 5 {
		t.Fatalf("woke %d times", woke)
	}
	info := w.Info()
	if len(info) != 4 || info[0].Fits || !info[1].Fits || info[1].Text != "&Tick me" || w.TextScale() < 1 {
		t.Fatalf("info %+v", info)
	}
	img, err := w.Capture()
	if err != nil || img.Bounds().Dx() < int(w.Scale(300)) {
		t.Fatalf("capture: %v", err)
	}
	cw, ch := w.ClientSize()
	if cw != int(w.Scale(300)) || ch != int(w.Scale(200)) {
		t.Fatalf("client %dx%d at %d dpi", cw, ch, w.DPI())
	}
	label.Show(false)
	if label.Visible() {
		t.Fatal("hidden label visible")
	}
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=4d09e126cc6046b56ee44084ebb94f2f0c262532f49c8dd2ebb6d210b333d65c ====
