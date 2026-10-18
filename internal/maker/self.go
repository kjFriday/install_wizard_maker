package maker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"acme/wizard/icon"
	"acme/wizard/internal/app"
	"acme/wizard/internal/install"
	"acme/wizard/internal/reg"
	"acme/wizard/internal/shell"
	"acme/wizard/internal/spec"
)

// License is the wizard maker's own license text; main embeds LICENSE.
var License string

// SelfSpec is the setup.json the maker installs itself with, its files written into dir: the running
// exe, the license and the icon. It is the same program as setup.json in the repository describes.
func SelfSpec(self, dir string) (*spec.Spec, error) {
	exe, err := os.ReadFile(self)
	if err != nil {
		return nil, err
	}
	for name, data := range map[string][]byte{app.ExeName: exe, "LICENSE": []byte(License), "icon.ico": icon.ICO} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return nil, err
		}
	}
	s := spec.New(dir)
	s.ID, s.Name, s.Version, s.Publisher, s.Group = app.ID, app.Name, app.Version, app.Publisher, app.Group
	s.Icon, s.License, s.Output = "icon.ico", "LICENSE", "setup.exe"
	s.Files = []spec.FileRule{{From: app.ExeName}}
	s.Run.Program = app.ExeName
	return s, nil
}

// selfSetup builds the maker's own setup.exe in a new temp folder; cleanup removes the folder.
func selfSetup(self string) (setup string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "acme-wizard-maker-")
	if err != nil {
		return "", nil, err
	}
	cleanup = func() { os.RemoveAll(dir) }
	s, err := SelfSpec(self, dir)
	if err == nil {
		var stub []byte
		if stub, err = os.ReadFile(self); err == nil {
			_, err = Build(s, stub, nil)
		}
	}
	if err != nil {
		cleanup()
		return "", nil, err
	}
	return filepath.Join(dir, "setup.exe"), cleanup, nil
}

// installSelf installs this copy of the maker for the current user: it builds its own setup.exe and
// runs it with args (-quiet, -desktop-shortcut, -root ...), waiting for it.
func installSelf(self string, args []string, printf func(string, ...any)) int {
	setup, cleanup, err := selfSetup(self)
	if err != nil {
		printf("%v", err)
		return app.ExitFailed
	}
	defer cleanup()
	return runAndWait(setup, args, printf)
}

// uninstallSelf runs the uninstaller of the installed maker with args, waiting for it. It refuses when
// self runs from the install folder: Windows will not delete a running exe, so the uninstall would stop
// half-way.
func uninstallSelf(self, root string, args []string, printf func(string, ...any)) int {
	dir, err := installedDir(root)
	if err != nil {
		printf("%v", err)
		return app.ExitFailed
	}
	if dir == "" {
		printf("%s is not installed for you.", app.Name)
		return app.ExitOK
	}
	uninstaller := filepath.Join(dir, install.UninstallerName)
	if runsFrom(self, dir) {
		printf("This copy runs from %s, which its uninstaller removes: uninstall %s from Apps & features, or run %s.",
			dir, app.Name, uninstaller)
		return app.ExitRefused
	}
	return runAndWait(uninstaller, args, printf)
}

// runsFrom reports whether the exe self lies in dir or a folder under it.
func runsFrom(self, dir string) bool {
	a, err1 := filepath.Abs(self)
	b, err2 := filepath.Abs(dir)
	if err1 != nil || err2 != nil {
		return false
	}
	rel, err := filepath.Rel(strings.ToLower(b), strings.ToLower(a))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// installedDir is where the maker is installed ("" if it is not), in the -root sandbox if root is set.
func installedDir(root string) (string, error) {
	var pl install.Places
	var r install.Registry
	if root != "" {
		pl = install.SandboxPlaces(root)
		r = reg.File{Path: filepath.Join(pl.Root, "registry.json")}
	} else {
		var err error
		if pl, err = shell.UserPlaces(); err != nil {
			return "", err
		}
		r = reg.HKCU{}
	}
	dir, _ := install.Existing(r, pl, app.ID, app.Group, app.Name)
	return dir, nil
}

func runAndWait(exe string, args []string, printf func(string, ...any)) int {
	cmd := exec.Command(exe, args...)
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		printf("%s", string(out))
	}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		printf("%v", err)
		return app.ExitFailed
	}
	return app.ExitOK
}

// startSelfSetup builds the maker's setup.exe and starts it without waiting (from the window); the
// temp folder goes once it exits.
func startSelfSetup(self string) error {
	setup, cleanup, err := selfSetup(self)
	if err != nil {
		return err
	}
	cmd := exec.Command(setup)
	if err := cmd.Start(); err != nil {
		cleanup()
		return err
	}
	go func() {
		cmd.Wait()
		cleanup()
	}()
	return nil
}

// startUninstaller starts the installed maker's uninstaller without waiting.
func startUninstaller() error {
	dir, err := installedDir("")
	if err != nil {
		return err
	}
	if dir == "" {
		return fmt.Errorf("%s is not installed for you", app.Name)
	}
	return exec.Command(filepath.Join(dir, install.UninstallerName)).Start()
}
