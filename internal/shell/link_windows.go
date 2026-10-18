// ==== ACME Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/shell/link_windows.go
// About: Part of package shell in install_wizard_maker.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

package shell

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"acme/wizard/internal/install"
)

var (
	clsidShellLink     = MustGUID("{00021401-0000-0000-C000-000000000046}")
	iidShellLinkW      = MustGUID("{000214F9-0000-0000-C000-000000000046}")
	iidPersistFile     = MustGUID("{0000010B-0000-0000-C000-000000000046}")
	iidPropertyStore   = MustGUID("{886D8EEB-8CF2-4446-8D02-CDBA1DBDCF99}")
	pkeyAppUserModelID = propertyKey{MustGUID("{9F4C2855-9F79-4B39-A8D0-E1D42DE1D5F3}"), 5}

	folderUserPrograms = MustGUID("{5CD7AEE2-2219-4A67-B85D-6C9CE15660CB}") // %LOCALAPPDATA%\Programs
	folderStartMenu    = MustGUID("{A77F5D77-2E2B-44C3-A6A2-ABA601054A51}") // Start menu\Programs
	folderDesktop      = MustGUID("{B4BFCC3A-DB2C-424C-B029-7FE99A87C641}")
	folderLocalAppData = MustGUID("{F1B32785-6FBA-4FCF-9D55-7B8E7F157091}")
	folderAppData      = MustGUID("{3EB685DB-65F9-4CF6-A03A-E3EF65729F3D}")

	procSHGetKnownFolderPath = shell32.NewProc("SHGetKnownFolderPath")
)

// IShellLinkW, IPersistFile and IPropertyStore methods used here, by vtable index.
const (
	linkGetPath             = 3
	linkGetDescription      = 6
	linkSetDescription      = 7
	linkGetWorkingDirectory = 8
	linkSetWorkingDirectory = 9
	linkGetArguments        = 10
	linkSetArguments        = 11
	linkGetIconLocation     = 16
	linkSetIconLocation     = 17
	linkSetPath             = 20
	fileLoad                = 5
	fileSave                = 6
	storeGetValue           = 5
	storeSetValue           = 6
	storeCommit             = 7
	vtLPWSTR                = 31
)

type propertyKey struct {
	fmtid GUID
	pid   uint32
}

// propVariant is PROPVARIANT for a VT_LPWSTR value (24 bytes on 64-bit Windows).
type propVariant struct {
	vt       uint16
	reserved [3]uint16
	str      *uint16
	_        uintptr
}

// KnownFolder is a known folder's path; it need not exist yet.
func KnownFolder(id GUID) (string, error) {
	var p *uint16
	r, _, _ := procSHGetKnownFolderPath.Call(uintptr(unsafe.Pointer(&id)), 0x4000, 0, uintptr(unsafe.Pointer(&p))) // KF_FLAG_DONT_VERIFY
	if int32(r) < 0 {
		return "", hrError("SHGetKnownFolderPath", int32(r))
	}
	return fromTaskMem(p), nil
}

// UserPlaces are this user's real folders (a redirected Desktop, e.g. to OneDrive, is followed).
func UserPlaces() (install.Places, error) {
	var pl install.Places
	for _, f := range []struct {
		id  GUID
		out *string
	}{{folderUserPrograms, &pl.Programs}, {folderStartMenu, &pl.StartMenu}, {folderDesktop, &pl.Desktop},
		{folderLocalAppData, &pl.LocalAppData}, {folderAppData, &pl.AppData}} {
		p, err := KnownFolder(f.id)
		if err != nil {
			return pl, err
		}
		*f.out = p
	}
	return pl, nil
}

// CreateLink writes a .lnk file (its folder must exist), with the AppUserModelID when one is given.
func CreateLink(s install.Shortcut) error {
	return withCOM(func() error {
		link, err := create(&clsidShellLink, &iidShellLinkW)
		if err != nil {
			return err
		}
		defer link.release()
		for _, set := range []struct {
			method int
			value  string
		}{{linkSetPath, s.Target}, {linkSetArguments, s.Args}, {linkSetWorkingDirectory, s.WorkDir}, {linkSetDescription, s.Description}} {
			if hr := link.call(set.method, uintptr(unsafe.Pointer(utf16(set.value)))); hr < 0 {
				return hrError("IShellLink", hr)
			}
		}
		if s.Icon != "" {
			if hr := link.call(linkSetIconLocation, uintptr(unsafe.Pointer(utf16(s.Icon))), 0); hr < 0 {
				return hrError("SetIconLocation", hr)
			}
		}
		if s.AppUserModelID != "" {
			store, err := link.query(&iidPropertyStore)
			if err != nil {
				return err
			}
			defer store.release()
			pv := propVariant{vt: vtLPWSTR, str: utf16(s.AppUserModelID)}
			if hr := store.call(storeSetValue, uintptr(unsafe.Pointer(&pkeyAppUserModelID)), uintptr(unsafe.Pointer(&pv))); hr < 0 {
				return hrError("IPropertyStore.SetValue", hr)
			}
			if hr := store.call(storeCommit); hr < 0 {
				return hrError("IPropertyStore.Commit", hr)
			}
		}
		file, err := link.query(&iidPersistFile)
		if err != nil {
			return err
		}
		defer file.release()
		if hr := file.call(fileSave, uintptr(unsafe.Pointer(utf16(s.Path))), 1); hr < 0 {
			return hrError("IPersistFile.Save", hr)
		}
		return nil
	})
}

// ReadLink reads a .lnk file back (for tests and checks).
func ReadLink(path string) (install.Shortcut, error) {
	s := install.Shortcut{Path: path}
	if _, err := os.Stat(path); err != nil {
		return s, err
	}
	err := withCOM(func() error {
		link, err := create(&clsidShellLink, &iidShellLinkW)
		if err != nil {
			return err
		}
		defer link.release()
		file, err := link.query(&iidPersistFile)
		if err != nil {
			return err
		}
		defer file.release()
		if hr := file.call(fileLoad, uintptr(unsafe.Pointer(utf16(path))), 0); hr < 0 {
			return hrError("IPersistFile.Load", hr)
		}
		buf := make([]uint16, 2048)
		get := func(method int, extra ...uintptr) string {
			buf[0] = 0
			link.call(method, append([]uintptr{uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))}, extra...)...)
			return syscall.UTF16ToString(buf)
		}
		s.Target = get(linkGetPath, 0, 4) // SLGP_RAWPATH
		s.Args = get(linkGetArguments)
		s.WorkDir = get(linkGetWorkingDirectory)
		s.Description = get(linkGetDescription)
		var index int32
		s.Icon = get(linkGetIconLocation, uintptr(unsafe.Pointer(&index)))
		store, err := link.query(&iidPropertyStore)
		if err != nil {
			return err
		}
		defer store.release()
		var pv propVariant
		if hr := store.call(storeGetValue, uintptr(unsafe.Pointer(&pkeyAppUserModelID)), uintptr(unsafe.Pointer(&pv))); hr >= 0 && pv.vt == vtLPWSTR && pv.str != nil {
			n := 0
			for *(*uint16)(unsafe.Add(unsafe.Pointer(pv.str), 2*n)) != 0 {
				n++
			}
			s.AppUserModelID = syscall.UTF16ToString(unsafe.Slice(pv.str, n))
		}
		procPropVariantClear.Call(uintptr(unsafe.Pointer(&pv)))
		return nil
	})
	return s, err
}

// MakeLinkDir is install.Env.Link: makes sure the folder exists, then writes the shortcut.
func MakeLinkDir(s install.Shortcut) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	if err := CreateLink(s); err != nil {
		return err
	}
	if _, err := os.Stat(s.Path); err != nil {
		return errors.New("the shortcut was not written")
	}
	return nil
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=9b47b9407e500d8f21e44263c72ccf31421717ce3f0129eb1572d8a2c0775a7b ====
