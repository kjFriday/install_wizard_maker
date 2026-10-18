package install

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"acme/wizard/internal/spec"
)

// UEnv is what an uninstall needs.
type UEnv struct {
	Places   Places
	Registry Registry
	Self     string // the running uninstall.exe: it can only go once it has exited
	Purge    bool   // also remove the program's settings folders (the marker's purge entries)
	Progress func(done, total int64, file string)
}

// Uninstall removes the install in dir that m describes; the caller checks m names its own program.
// Only the marker's shortcuts and files are touched, and a file is removed only if it is still exactly
// as installed (an entry without a hash, such as another installer's, is never removed). The registry
// entry goes last, so a failed uninstall can simply be run again.
func Uninstall(dir string, m *Marker, env UEnv) (Result, error) {
	var res Result
	for _, s := range m.Shortcuts {
		if !env.Places.ownsLink(s) {
			continue
		}
		if err := os.Remove(s); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return res, err
		}
		removeEmptyParents(s, env.Places.StartMenu)
	}
	self, _ := filepath.Abs(env.Self)
	for i, raw := range m.Files {
		e, ok := entry(raw)
		if !ok {
			continue
		}
		if env.Progress != nil {
			env.Progress(int64(i), int64(len(m.Files)), e)
		}
		path := filepath.Join(dir, filepath.FromSlash(e))
		if strings.EqualFold(path, self) {
			continue
		}
		want := m.SHA256[raw]
		if want == "" {
			want = m.SHA256[e]
		}
		got, err := fileSum(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if want == "" || err != nil || got != want {
			res.Kept = append(res.Kept, e)
			continue
		}
		if err := os.Remove(path); err != nil {
			return res, err
		}
		removeEmptyParents(path, dir)
	}
	if err := os.Remove(filepath.Join(dir, MarkerName)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return res, err
	}
	if env.Purge {
		for _, p := range m.Purge {
			if path, err := spec.PurgePath(p, env.Places.LocalAppData, env.Places.AppData); err == nil {
				os.RemoveAll(path)
			}
		}
	}
	if err := env.Registry.Delete(m.AppID); err != nil {
		return res, err
	}
	group := filepath.Dir(dir)
	if !within(env.Places.Programs, group) {
		group = ""
	}
	if strings.EqualFold(filepath.Dir(self), dir) {
		res.AfterExit = []string{self, dir}
		if group != "" {
			res.AfterExit = append(res.AfterExit, group)
		}
	} else {
		os.Remove(dir) // only when empty
		if group != "" {
			os.Remove(group)
		}
	}
	if env.Progress != nil {
		env.Progress(int64(len(m.Files)), int64(len(m.Files)), "")
	}
	return res, nil
}

// DeleteAfterExit starts a hidden cmd that waits for this process to end, deletes file, then removes
// the folders if they are empty (never with /s: kept files stay). The paths travel in environment
// variables, quoted, so no character in a folder name can change the command.
func DeleteAfterExit(file string, dirs ...string) error {
	env := append(os.Environ(), "ACME_DEL="+file)
	try := `del /f /q "%ACME_DEL%" 2>nul`
	script := `ping -n 3 127.0.0.1 >nul & ` + try +
		` & (if exist "%ACME_DEL%" (ping -n 3 127.0.0.1 >nul & ` + try + `))` +
		` & (if exist "%ACME_DEL%" (ping -n 6 127.0.0.1 >nul & ` + try + `))`
	for i, d := range dirs {
		v := "ACME_RD" + string(rune('1'+i))
		env = append(env, v+"="+d)
		script += ` & rd "%` + v + `%" 2>nul`
	}
	cmd := exec.Command("cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd.exe /d /c "` + script + `"`, HideWindow: true,
		CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	cmd.Env = env
	cmd.Dir = os.TempDir() // not the folder being removed
	return cmd.Start()
}
