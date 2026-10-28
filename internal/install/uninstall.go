// ==== Generic Company Inc. portfolio ====================================
// Project: install_wizard_maker | File: internal/install/uninstall.go
// About: ==== Generic Company Inc. portfolio ===============================================...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// ==== Generic Company Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/install/uninstall.go
// About: Part of package install in install_wizard_maker.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

package install

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"generic-company/wizard/internal/spec"
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
		if err := removeWait(path); err != nil {
			return res, fmt.Errorf("%w; close it and run the uninstaller again", err)
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

// lockWait is how long removeWait retries a file that is in use.
var lockWait = 10 * time.Second

// removeWait removes path, retrying while it is in use: a program that started its uninstaller and is
// closing (the wizard maker's Help > Uninstall) still holds its exe for a moment.
func removeWait(path string) error {
	deadline := time.Now().Add(lockWait)
	for {
		err := os.Remove(path)
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// DeleteAfterExit starts a hidden cmd that waits for this process to end, deletes file, then removes
// the folders if they are empty (never with /s: kept files stay). The paths travel in environment
// variables, quoted, so no character in a folder name can change the command.
func DeleteAfterExit(file string, dirs ...string) error {
	env := append(os.Environ(), "GENERIC_COMPANY_DEL="+file)
	try := `del /f /q "%GENERIC_COMPANY_DEL%" 2>nul`
	script := `ping -n 3 127.0.0.1 >nul & ` + try +
		` & (if exist "%GENERIC_COMPANY_DEL%" (ping -n 3 127.0.0.1 >nul & ` + try + `))` +
		` & (if exist "%GENERIC_COMPANY_DEL%" (ping -n 6 127.0.0.1 >nul & ` + try + `))`
	for i, d := range dirs {
		v := "GENERIC_COMPANY_RD" + string(rune('1'+i))
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

// ==== stamp v1 | author=kjFriday | hmac-sha256=5818bf802e88bdc3a74178bd45e9415c43af1b23e658c97ab900f7d96ed535c8 ====
