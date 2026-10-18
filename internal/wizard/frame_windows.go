package wizard

import (
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"acme/wizard/internal/win"
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

// smokeReport is what -smoke writes: every page as the user saw it, captured, with its controls.
type smokeReport struct {
	Role      string      `json:"role"`
	Title     string      `json:"title"`
	DPI       uint32      `json:"dpi"`
	TextScale float64     `json:"textScale"`
	Font      string      `json:"font"`
	IconSet   bool        `json:"iconSet"`
	Client    [2]int      `json:"client"`
	Pages     []smokePage `json:"pages"`
	Exit      int         `json:"exit"`
	Panic     string      `json:"panic,omitempty"`
}

type smokePage struct {
	Name     string            `json:"name"`
	PNG      string            `json:"png"`
	Next     bool              `json:"next"` // Next enabled
	Back     bool              `json:"back"` // Back enabled
	Controls []win.ControlInfo `json:"controls"`
	Error    string            `json:"error,omitempty"` // why there is no PNG
}

type smoker struct {
	f      *frame
	file   string
	report smokeReport
}

func newSmoker(f *frame, file, role string) *smoker {
	os.MkdirAll(filepath.Dir(file), 0o755)
	cw, ch := f.w.ClientSize()
	return &smoker{f: f, file: file, report: smokeReport{Role: role, Title: f.title, DPI: f.w.DPI(),
		TextScale: f.w.TextScale(), Font: f.w.FontFace(), IconSet: f.w.IconSet(), Client: [2]int{cw, ch}}}
}

// settle lets the window handle everything pending, paint, and finish the buttons' fade animations.
func (s *smoker) settle() {
	for i := 0; i < 20; i++ {
		s.f.w.Pump()
		time.Sleep(15 * time.Millisecond)
	}
}

// capture saves the window as <report>-NN-<name>.png beside the report and records its controls.
func (s *smoker) capture(name string) {
	s.settle()
	base := strings.TrimSuffix(s.file, filepath.Ext(s.file))
	file := fmt.Sprintf("%s-%02d-%s.png", base, len(s.report.Pages)+1, name)
	page := smokePage{Name: name, PNG: filepath.Base(file), Next: s.f.next.Enabled(), Back: s.f.back.Enabled(), Controls: s.f.w.Info()}
	img, err := s.f.w.Capture()
	for try := 0; err != nil && try < 4; try++ { // the very first capture can come too early
		s.settle()
		img, err = s.f.w.Capture()
	}
	if err == nil {
		var out *os.File
		if out, err = os.Create(file); err == nil {
			err = png.Encode(out, img)
			out.Close()
		}
	}
	if err != nil {
		page.PNG, page.Error = "", err.Error()
	}
	s.report.Pages = append(s.report.Pages, page)
}

// waitFor pumps messages until done reports true (or a minute passes).
func (s *smoker) waitFor(done func() bool) {
	for deadline := time.Now().Add(time.Minute); !done() && time.Now().Before(deadline) && !s.f.w.Closed(); {
		s.f.w.Pump()
		time.Sleep(20 * time.Millisecond)
	}
}

func (s *smoker) write(exit int) error {
	s.report.Exit = exit
	if s.f.w.Panic != nil {
		s.report.Panic = s.f.w.Panic.Error()
	}
	b, err := json.MarshalIndent(s.report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.file, b, 0o644)
}
