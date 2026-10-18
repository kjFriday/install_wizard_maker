package maker

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"acme/wizard/internal/app"
	"acme/wizard/internal/spec"
	"acme/wizard/internal/sys"
)

const usage = `Usage:
  wizard-maker.exe build [-o out.exe] [-quiet] setup.json   make the setup.exe setup.json describes
  wizard-maker.exe -version                                 print the version`

// out is where command-line text goes: the console or pipe there is, else one message box at the end.
type out struct {
	w     io.Writer
	buf   strings.Builder
	quiet bool
}

func newOut(quiet bool) *out { return &out{w: sys.Stdout(), quiet: quiet} }

func (o *out) printf(format string, a ...any) {
	s := fmt.Sprintf(format, a...) + "\n"
	if o.w != nil {
		io.WriteString(o.w, s)
	} else {
		o.buf.WriteString(s)
	}
}

func (o *out) done(icon uint32) {
	if o.w == nil && !o.quiet && o.buf.Len() > 0 {
		sys.MessageBox(app.Name, strings.TrimSpace(o.buf.String()), icon)
	}
}

// Main is the maker's command line. self is its own exe, the program part of every setup.exe.
func Main(self string, args []string) int {
	if len(args) > 0 && args[0] == "build" {
		return buildCommand(self, args[1:])
	}
	fs := flag.NewFlagSet(app.ExeName, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	version := fs.Bool("version", false, "print the version")
	err := fs.Parse(args)
	o := newOut(false)
	switch {
	case err != nil || fs.NArg() > 0:
		if err == nil {
			err = fmt.Errorf("unexpected %q", fs.Arg(0))
		}
		o.printf("%v\n\n%s", err, usage)
		o.done(sys.Error)
		return app.ExitUsage
	case *version:
		o.printf("%s %s", app.Name, app.Version)
		o.done(sys.Info)
		return app.ExitOK
	}
	o.printf("%s\n\n%s", app.Title, usage)
	o.done(sys.Info)
	return app.ExitOK
}

func buildCommand(self string, args []string) int {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	output := fs.String("o", "", "where to write the setup.exe (default: output in setup.json)")
	quiet := fs.Bool("quiet", false, "no message box")
	err := fs.Parse(args)
	o := newOut(*quiet)
	if err != nil || fs.NArg() != 1 {
		if err == nil && fs.NArg() > 1 {
			err = errors.New("flags go before setup.json")
		} else if err == nil {
			err = errors.New("which setup.json?")
		}
		o.printf("%v\n\n%s", err, usage)
		o.done(sys.Error)
		return app.ExitUsage
	}
	s, err := spec.Load(fs.Arg(0))
	if err != nil {
		o.printf("%v", err)
		o.done(sys.Error)
		return app.ExitUsage
	}
	if *output != "" {
		if s.Output, err = filepath.Abs(*output); err != nil {
			o.printf("%v", err)
			o.done(sys.Error)
			return app.ExitUsage
		}
	}
	stub, err := os.ReadFile(self)
	if err != nil {
		o.printf("%v", err)
		o.done(sys.Error)
		return app.ExitFailed
	}
	if _, err := Build(s, stub, o.printf); err != nil {
		var se *SpecError
		if errors.As(err, &se) {
			o.printf("%s has problems:\n  %s", filepath.Base(fs.Arg(0)), strings.ReplaceAll(err.Error(), "\n", "\n  "))
			o.done(sys.Error)
			return app.ExitUsage
		}
		o.printf("Build failed: %v", err)
		o.done(sys.Error)
		return app.ExitFailed
	}
	o.done(sys.Info)
	return app.ExitOK
}
