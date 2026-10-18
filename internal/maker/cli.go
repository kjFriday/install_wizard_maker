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
  wizard-maker.exe [setup.json]                             open the window
  wizard-maker.exe build [-o out.exe] [-quiet] setup.json   make the setup.exe setup.json describes
  wizard-maker.exe -version                                 print the version
  wizard-maker.exe -smoke FILE [setup.json]                 open, build, capture, report to FILE, close`

// Main is the maker's command line. self is its own exe, the program part of every setup.exe.
func Main(self string, args []string) int {
	if len(args) > 0 && args[0] == "build" {
		return buildCommand(self, args[1:])
	}
	fs := flag.NewFlagSet(app.ExeName, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	version := fs.Bool("version", false, "print the version")
	smoke := fs.String("smoke", "", "open the window, build, write a report to this file, close")
	err := fs.Parse(args)
	o, _ := sys.NewOutput(app.Name, false, "")
	switch {
	case err != nil || fs.NArg() > 1:
		if err == nil {
			err = fmt.Errorf("unexpected %q", fs.Arg(1))
		}
		o.Printf("%v\n\n%s", err, usage)
		o.Done(sys.Error)
		return app.ExitUsage
	case *version:
		o.Printf("%s %s", app.Name, app.Version)
		o.Done(sys.Info)
		return app.ExitOK
	}
	return Window(self, fs.Arg(0), *smoke)
}

func buildCommand(self string, args []string) int {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	output := fs.String("o", "", "where to write the setup.exe (default: output in setup.json)")
	quiet := fs.Bool("quiet", false, "no message box")
	err := fs.Parse(args)
	o, _ := sys.NewOutput(app.Name, *quiet, "")
	fail := func(code int, format string, a ...any) int {
		o.Printf(format, a...)
		o.Done(sys.Error)
		return code
	}
	if err != nil || fs.NArg() != 1 {
		if err == nil && fs.NArg() > 1 {
			err = errors.New("flags go before setup.json")
		} else if err == nil {
			err = errors.New("which setup.json?")
		}
		return fail(app.ExitUsage, "%v\n\n%s", err, usage)
	}
	s, err := spec.Load(fs.Arg(0))
	if err != nil {
		return fail(app.ExitUsage, "%v", err)
	}
	if *output != "" {
		if s.Output, err = filepath.Abs(*output); err != nil {
			return fail(app.ExitUsage, "%v", err)
		}
	}
	stub, err := os.ReadFile(self)
	if err != nil {
		return fail(app.ExitFailed, "%v", err)
	}
	if _, err := Build(s, stub, o.Printf); err != nil {
		var se *SpecError
		if errors.As(err, &se) {
			return fail(app.ExitUsage, "%s has problems:\n  %s", filepath.Base(fs.Arg(0)), strings.ReplaceAll(err.Error(), "\n", "\n  "))
		}
		return fail(app.ExitFailed, "Build failed: %v", err)
	}
	o.Done(sys.Info)
	return app.ExitOK
}
