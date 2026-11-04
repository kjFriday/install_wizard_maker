// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/wizard/frame_windows.go
// About: The wizard chrome that setup and the uninstaller share: pages, banner or header, buttons.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// The wizard chrome that setup and the uninstaller share: pages, banner or header, buttons.
//
// setupWindow (setup_windows.go) and uninstallWindow (uninstall_windows.go) each make a frame with
// newFrame, add their pages' controls (header or exterior, then add), call buttons last, and call
// show after every move of the Flow. The frame paints the backgrounds itself: on exterior pages a
// dark banner down the left with the program's icon at 128 units, on interior pages a white header
// strip with the icon at 48 units on the right.
//
// The layout is the classic Windows wizard's size, 503×362 in win.Rect's 96-dpi units.

package wizard

import (
	"image"

	"generic-company/wizard/internal/win"
)

// The wizard's layout, in 96-dpi units (the classic wizard size), and its command ids.
//
// The buttons' x positions (241, 316, 408) are written out in buttons, not derived from these. Next
// and Cancel use IDOK and IDCANCEL, so Enter and Esc reach them through IsDialogMessage.
const (
	winW, winH  = 503, 362 // the client area
	bannerW     = 164      // the exterior pages' side banner
	headerH     = 58       // the interior pages' white header
	lineY       = 314      // the separator above the buttons
	btnY        = 327
	btnW, btnH  = 75, 23
	bodyX       = bannerW + 16        // an exterior page's text, right of the banner
	bodyW       = winW - bannerW - 32 // 16 units of margin each side
	idBack      = 3
	idFolder    = 10
	idBrowse    = 11
	idAccept    = 12
	idStartMenu = 13
	idDesktop   = 14
	idLaunch    = 15
	idPurge     = 16
)

// frame is what the install wizard and the uninstaller share: pages of controls, a side banner or a
// white header, and Back / Next / Cancel.
//
// The setup and uninstall windows embed it; like the window, it is used on the UI thread only.
type frame struct {
	w                  *win.Window
	flow               *Flow
	icon               image.Image             // the program's icon, the largest image of its .ico
	pages              map[Page][]*win.Control // each page's controls, shown only on that page
	back, next, cancel *win.Control
	gate               Gate   // what Next waits for; the embedding window keeps it current
	title              string // the window title, also the message boxes' title
}

// newFrame makes the wizard's window (hidden) with the program's icon ico and sets it to paint the
// banner or header. The error is the window's.
func newFrame(title string, flow *Flow, ico []byte) (*frame, error) {
	w, err := win.New(title, winW, winH, 0)
	if err != nil {
		return nil, err
	}
	f := &frame{w: w, flow: flow, icon: win.LargestIcon(ico), pages: map[Page][]*win.Control{}, title: title}
	w.OnPaint = f.paint
	return f, nil
}

// buttons comes after the pages' controls, so Tab reaches them last.
func (f *frame) buttons() {
	f.w.Separator(win.Rect{X: 0, Y: lineY, W: winW, H: 2})
	f.back = f.w.Button(idBack, "< &Back", win.Rect{X: 241, Y: btnY, W: btnW, H: btnH})
	f.next = f.w.Button(win.IDOK, "&Next >", win.Rect{X: 316, Y: btnY, W: btnW, H: btnH})
	f.cancel = f.w.Button(win.IDCANCEL, "Cancel", win.Rect{X: 408, Y: btnY, W: btnW, H: btnH})
	f.w.SetDefault(win.IDOK)
}

// add puts controls on page p, so show shows them only there.
func (f *frame) add(p Page, cs ...*win.Control) { f.pages[p] = append(f.pages[p], cs...) }

// header gives an interior page its title and subtitle.
func (f *frame) header(p Page, title, sub string) {
	f.add(p, f.w.Label(title, win.Rect{X: 20, Y: 9, W: 410, H: 20}, win.Bold, win.Paper),
		f.w.Label(sub, win.Rect{X: 36, Y: 30, W: 395, H: 26}, win.Normal, win.Paper),
		f.w.Separator(win.Rect{X: 0, Y: headerH, W: winW, H: 2}))
}

// exterior gives a banner page its big title and body; it returns them to be filled in later.
func (f *frame) exterior(p Page, title, body string) (*win.Control, *win.Control) {
	t := f.w.Label(title, win.Rect{X: bodyX, Y: 18, W: bodyW, H: 52}, win.Title, win.Paper)
	b := f.w.Label(body, win.Rect{X: bodyX, Y: 82, W: bodyW, H: 150}, win.Normal, win.Paper)
	f.add(p, t, b)
	return t, b
}

// show shows the current page and sets the buttons for it.
//
// Other pages' controls are hidden first, then the current page's shown. Back disappears on the last
// page, Cancel is disabled there, and Next takes the focus when it is enabled, so Enter goes on.
func (f *frame) show() {
	page := f.flow.Page()
	for p, cs := range f.pages {
		if p != page {
			for _, c := range cs {
				c.Show(false)
			}
		}
	}
	for _, c := range f.pages[page] {
		c.Show(true)
	}
	f.back.Show(!f.flow.Last())
	f.back.Enable(f.flow.CanBack())
	f.next.SetText(f.flow.NextLabel())
	f.next.Enable(f.flow.CanNext(f.gate))
	f.cancel.Enable(!f.flow.Last())
	f.w.Invalidate()
	if f.next.Enabled() {
		f.next.Focus()
	}
}

// paint draws the backgrounds of the current page (the window's OnPaint).
//
// Exterior pages: the banner left of bannerW, white paper right of it, both down to the separator,
// and the icon at 128 units centred in the banner. Interior pages: the white header strip with the
// icon at 48 units, 8 from the right edge. The dialog colour elsewhere is the class background.
func (f *frame) paint(p *win.Painter) {
	if f.flow.Page().Exterior() {
		p.Fill(win.Rect{X: 0, Y: 0, W: bannerW, H: lineY}, win.Banner)
		p.Fill(win.Rect{X: bannerW, Y: 0, W: winW - bannerW, H: lineY}, win.Paper)
		p.Pixels(f.icon, (bannerW-128)/2, 36, 128)
		return
	}
	p.Fill(win.Rect{X: 0, Y: 0, W: winW, H: headerH}, win.Paper)
	p.Pixels(f.icon, winW-56, 5, 48)
}

// ask asks a yes-or-no question in a message box; No is the default button, so a stray Enter keeps
// things as they are. It reports whether the user chose Yes.
func (f *frame) ask(text string) bool {
	return f.w.MessageBox(f.title, text, win.MBYesNo|win.MBIconQuestion|win.MBDefButton2) == win.IDYES
}

// flags are what a -smoke capture records about the buttons.
func (f *frame) flags() map[string]bool {
	return map[string]bool{"next": f.next.Enabled(), "back": f.back.Enabled()}
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=81bf23a1191c5ca9cb77181b3f50ab3a367af6511fe4eec8ef87078758668157 ====
