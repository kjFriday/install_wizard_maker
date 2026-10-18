package win

import (
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SmokeReport is what a -smoke run writes: every step as the user saw it, captured, with its controls.
type SmokeReport struct {
	Role      string      `json:"role"`
	Title     string      `json:"title"`
	DPI       uint32      `json:"dpi"`
	TextScale float64     `json:"textScale"`
	Font      string      `json:"font"`
	IconSet   bool        `json:"iconSet"`
	Client    [2]int      `json:"client"`
	Pages     []SmokePage `json:"pages"`
	Exit      int         `json:"exit"`
	Panic     string      `json:"panic,omitempty"`
}

// SmokePage is one captured step.
type SmokePage struct {
	Name     string          `json:"name"`
	PNG      string          `json:"png"`
	Flags    map[string]bool `json:"flags,omitempty"` // e.g. whether Next was enabled
	Controls []ControlInfo   `json:"controls"`
	Error    string          `json:"error,omitempty"` // why there is no PNG
}

// Smoke drives a window through its steps and records them (the -smoke flag every role has).
type Smoke struct {
	w      *Window
	file   string
	Report SmokeReport
}

// NewSmoke starts a report written to file; captures go beside it as <file>-NN-<step>.png.
func NewSmoke(w *Window, file, role, title string) *Smoke {
	os.MkdirAll(filepath.Dir(file), 0o755)
	cw, ch := w.ClientSize()
	return &Smoke{w: w, file: file, Report: SmokeReport{Role: role, Title: title, DPI: w.DPI(), TextScale: w.TextScale(),
		Font: w.FontFace(), IconSet: w.IconSet(), Client: [2]int{cw, ch}}}
}

// Settle lets the window handle everything pending, paint, and finish the buttons' fade animations.
func (s *Smoke) Settle() {
	for i := 0; i < 20; i++ {
		s.w.Pump()
		time.Sleep(15 * time.Millisecond)
	}
}

// Capture saves the window as a PNG and records its controls under name.
func (s *Smoke) Capture(name string, flags map[string]bool) {
	s.Settle()
	base := strings.TrimSuffix(s.file, filepath.Ext(s.file))
	file := fmt.Sprintf("%s-%02d-%s.png", base, len(s.Report.Pages)+1, name)
	page := SmokePage{Name: name, PNG: filepath.Base(file), Flags: flags, Controls: s.w.Info()}
	img, err := s.w.Capture()
	for try := 0; err != nil && try < 4; try++ { // the very first capture can come too early
		s.Settle()
		img, err = s.w.Capture()
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
	s.Report.Pages = append(s.Report.Pages, page)
}

// WaitFor pumps messages until done reports true (or a minute passes, or the window closes).
func (s *Smoke) WaitFor(done func() bool) {
	for deadline := time.Now().Add(time.Minute); !done() && time.Now().Before(deadline) && !s.w.Closed(); {
		s.w.Pump()
		time.Sleep(20 * time.Millisecond)
	}
}

// Write writes the report.
func (s *Smoke) Write(exit int) error {
	s.Report.Exit = exit
	if s.w.Panic != nil {
		s.Report.Panic = s.w.Panic.Error()
	}
	b, err := json.MarshalIndent(s.Report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.file, b, 0o644)
}
