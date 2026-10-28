// ==== Generic Company Inc. portfolio ====================================
// Project: install_wizard_maker | File: internal/wizard/frame_windows.go
// About: ==== Generic Company Inc. portfolio ===============================================...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// ==== Generic Company Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/wizard/frame_windows.go
// About: Part of package wizard in install_wizard_maker.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

package wizard

import (
	"image"

	"generic-company/wizard/internal/win"
)

// The wizard's layout, in 96-dpi units (the classic wizard size), and its command ids.
const (
	winW, winH  = 503, 362
	bannerW     = 164
	headerH     = 58
	lineY       = 314
	btnY        = 327
	btnW, btnH  = 75, 23
	bodyX       = bannerW + 16
	bodyW       = winW - bannerW - 32
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
type frame struct {
	w                  *win.Window
	flow               *Flow
	icon               image.Image
	pages              map[Page][]*win.Control
	back, next, cancel *win.Control
	gate               Gate
	title              string
}

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

func (f *frame) ask(text string) bool {
	return f.w.MessageBox(f.title, text, win.MBYesNo|win.MBIconQuestion|win.MBDefButton2) == win.IDYES
}

// flags are what a -smoke capture records about the buttons.
func (f *frame) flags() map[string]bool {
	return map[string]bool{"next": f.next.Enabled(), "back": f.back.Enabled()}
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=23a44b48bbeba0f4bb906078e9c928fb6b211c870a1cc8366448d8bfcaaca259 ====
