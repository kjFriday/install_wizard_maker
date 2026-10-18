package wizard

import (
	"fmt"

	"acme/wizard/internal/app"
	"acme/wizard/internal/sys"
)

// findPython finds pythonw.exe for {python}; not built yet.
var findPython = func(min string) string { return "" }

// setupWindow: until the wizard window exists, ask once and install with the defaults.
func setupWindow(s *Setup, o setupOptions, out *sys.Output) int {
	if !sys.Ask(s.M.Name+" Setup", fmt.Sprintf("Install %s %s for you, in %s?", s.M.Name, s.M.Version, s.Choices.Dir)) {
		return app.ExitCancelled
	}
	return quietSetup(s, out)
}

// uninstallWindow: until the uninstaller window exists, ask once.
func uninstallWindow(u *Uninstaller, o uninstallOptions, out *sys.Output, release func()) int {
	defer release()
	if !sys.Ask(u.Info.Name+" Uninstall", fmt.Sprintf("Remove %s %s?", u.Info.Name, u.Info.Version)) {
		return app.ExitCancelled
	}
	res, err := u.Remove(o.purge, nil)
	if err != nil {
		out.Printf("%v", err)
		out.Done(sys.Error)
		return exitCode(err)
	}
	out.Printf("Removed %s.", u.Info.Name)
	out.Done(sys.Info)
	release()
	Finish(res)
	return app.ExitOK
}
