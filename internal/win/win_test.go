// ==== ACME Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/win/win_test.go
// About: Tests for package win.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

package win

import (
	"image"
	"image/color"
	"runtime"
	"testing"
	"time"
	"unsafe"
)

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

func TestWindowRoundTrip(t *testing.T) {
	runtime.LockOSThread()
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

// ==== stamp v1 | author=kjFriday | hmac-sha256=c9827db4fd67cde8b57de935730e551bc1d2aa44b67e7a960abc8d842500d9bd ====
