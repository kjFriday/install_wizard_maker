// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/shell/link_windows.go
// About: Shortcuts (.lnk files) and the user's known folders, through the Windows shell.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Shortcuts (.lnk files) and the user's known folders, through the Windows shell.
//
// The wizard and the maker call UserPlaces to learn where a per-user install goes, and the install
// engine writes each shortcut through MakeLinkDir (install.Env.Link). ReadLink exists for the
// tests. Everything here goes through the COM plumbing in com_windows.go.
//
// How a shortcut is written:
//  1. CoCreateInstance makes a ShellLink object and hands back its IShellLinkW interface.
//  2. IShellLinkW's setters fill in the target, arguments, working folder, tooltip and icon.
//  3. When an AppUserModelID is wanted, the same object is queried for IPropertyStore and the id
//     is stored under PKEY_AppUserModel_ID; the taskbar groups windows that carry the same id.
//  4. The object is queried for IPersistFile, whose Save writes the .lnk file.
//
// Known folders are found by id (a FOLDERID GUID from KnownFolders.h) with SHGetKnownFolderPath,
// which follows folder redirection, unlike building the paths from environment variables.

package shell

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"generic-company/wizard/internal/install"
)

// The COM ids, property key, known-folder ids and shell32 function this file uses.
//
// clsidShellLink is CLSID_ShellLink, the shell's shortcut object; iidShellLinkW, iidPersistFile and
// iidPropertyStore are the interfaces asked of it (ShObjIdl.h, ObjIdl.h, PropSys.h).
// pkeyAppUserModelID is PKEY_AppUserModel_ID (propkey.h): the property set id plus property id 5.
// The folder ids are FOLDERID values from KnownFolders.h.
var (
	clsidShellLink     = MustGUID("{00021401-0000-0000-C000-000000000046}")
	iidShellLinkW      = MustGUID("{000214F9-0000-0000-C000-000000000046}")
	iidPersistFile     = MustGUID("{0000010B-0000-0000-C000-000000000046}")
	iidPropertyStore   = MustGUID("{886D8EEB-8CF2-4446-8D02-CDBA1DBDCF99}")
	pkeyAppUserModelID = propertyKey{MustGUID("{9F4C2855-9F79-4B39-A8D0-E1D42DE1D5F3}"), 5}

	folderUserPrograms = MustGUID("{5CD7AEE2-2219-4A67-B85D-6C9CE15660CB}") // %LOCALAPPDATA%\Programs
	folderStartMenu    = MustGUID("{A77F5D77-2E2B-44C3-A6A2-ABA601054A51}") // Start menu\Programs
	folderDesktop      = MustGUID("{B4BFCC3A-DB2C-424C-B029-7FE99A87C641}") // FOLDERID_Desktop
	folderLocalAppData = MustGUID("{F1B32785-6FBA-4FCF-9D55-7B8E7F157091}") // FOLDERID_LocalAppData
	folderAppData      = MustGUID("{3EB685DB-65F9-4CF6-A03A-E3EF65729F3D}") // FOLDERID_RoamingAppData

	procSHGetKnownFolderPath = shell32.NewProc("SHGetKnownFolderPath")
)

// IShellLinkW, IPersistFile and IPropertyStore methods used here, by vtable index.
//
// Each index counts IUnknown's three methods first, then the interface's own in header order (for
// IPersistFile, IPersist's GetClassID is 3 and IsDirty is 4). vtLPWSTR is VT_LPWSTR, the
// PROPVARIANT type tag for a NUL-terminated UTF-16 string.
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

// propertyKey is PROPERTYKEY: a property set id (fmtid) and the property's number in that set
// (20 bytes, as TestSizes checks).
type propertyKey struct {
	fmtid GUID
	pid   uint32
}

// propVariant is PROPVARIANT for a VT_LPWSTR value (24 bytes on 64-bit Windows).
//
// The real type is a tag (vt), three reserved words, then a 16-byte union; only the union's first
// field, the string pointer, is used, and the blank uintptr pads the union to its full size.
type propVariant struct {
	vt       uint16
	reserved [3]uint16
	str      *uint16
	_        uintptr
}

// KnownFolder is a known folder's path; it need not exist yet.
//
// id is a FOLDERID value. KF_FLAG_DONT_VERIFY (0x4000) skips the check that the folder exists, so
// a folder that was never made (Programs on a fresh profile, say) still gets its path. The shell
// allocates the string; fromTaskMem copies and frees it. A negative HRESULT becomes the error.
func KnownFolder(id GUID) (string, error) {
	var p *uint16
	r, _, _ := procSHGetKnownFolderPath.Call(uintptr(unsafe.Pointer(&id)), 0x4000, 0, uintptr(unsafe.Pointer(&p))) // KF_FLAG_DONT_VERIFY
	if int32(r) < 0 {
		return "", hrError("SHGetKnownFolderPath", int32(r))
	}
	return fromTaskMem(p), nil
}

// UserPlaces are this user's real folders (a redirected Desktop, e.g. to OneDrive, is followed).
//
// The wizard, the uninstaller and the maker's self-install call it on a real computer (a -root
// sandbox uses install.SandboxPlaces instead). Root stays "". The first folder that cannot be
// resolved stops the lookup, and its error comes back with the folders found so far.
func UserPlaces() (install.Places, error) {
	var pl install.Places
	// each known folder id paired with the Places field it fills
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
//
// An existing file at s.Path is overwritten. Every COM call runs inside withCOM on one locked OS
// thread, and every interface is released before it returns. The first failing call's HRESULT is
// the error, and no file is written in that case.
//
// Steps:
//  1. Create the ShellLink object.
//  2. Set path, arguments, working folder and description, then the icon (index 0) when given.
//  3. With an AppUserModelID, store it through IPropertyStore and Commit it into the object.
//  4. Save through IPersistFile.
func CreateLink(s install.Shortcut) error {
	return withCOM(func() error {
		link, err := create(&clsidShellLink, &iidShellLinkW)
		if err != nil {
			return err
		}
		defer link.release()
		// the four string setters share one shape: method(LPCWSTR)
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
			// the string stays Go-owned: SetValue copies it, so no PropVariantClear here
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
		// Save(path, fRemember=TRUE): the object takes s.Path as its current file
		if hr := file.call(fileSave, uintptr(unsafe.Pointer(utf16(s.Path))), 1); hr < 0 {
			return hrError("IPersistFile.Save", hr)
		}
		return nil
	})
}

// ReadLink reads a .lnk file back (for tests and checks).
//
// A missing file is the os.Stat error. Once the file loads, the getters' HRESULTs are not checked:
// a field that cannot be read comes back "" (the buffer starts empty), and so does a missing
// AppUserModelID. The icon index is read but dropped, since Shortcut has no field for it.
//
// Known limitation: each field is read into a 2048-unit buffer, so a longer value comes back cut.
//
// Steps:
//  1. Create a ShellLink object and Load the file through IPersistFile (mode 0, STGM_READ).
//  2. Read target (raw, environment variables unexpanded), arguments, folder, tooltip and icon.
//  3. Read the AppUserModelID from IPropertyStore and free the PROPVARIANT.
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
		// get calls a getter of the shape method(buffer, buffer length in UTF-16 units, extra...)
		get := func(method int, extra ...uintptr) string {
			buf[0] = 0
			link.call(method, append([]uintptr{uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))}, extra...)...)
			return syscall.UTF16ToString(buf)
		}
		// GetPath's extra arguments: no WIN32_FIND_DATAW (0), then the flags
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
			// count UTF-16 units up to the NUL, as fromTaskMem does, but leave freeing to PropVariantClear
			n := 0
			for *(*uint16)(unsafe.Add(unsafe.Pointer(pv.str), 2*n)) != 0 {
				n++
			}
			s.AppUserModelID = syscall.UTF16ToString(unsafe.Slice(pv.str, n))
		}
		// GetValue hands back a copy the caller owns; clearing an empty (VT_EMPTY) value is harmless
		procPropVariantClear.Call(uintptr(unsafe.Pointer(&pv)))
		return nil
	})
	return s, err
}

// MakeLinkDir is install.Env.Link: makes sure the folder exists, then writes the shortcut.
//
// It creates any missing parent folders (a new Start menu group, say). After CreateLink it checks
// that the file is really there, so a save that reports success without a file still counts as a
// failure and the install rolls back.
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

// ==== stamp v1 | author=kjFriday | hmac-sha256=e069336df659e8aacbaeb03b2928bd9fb5bd2f42395f88aea964a5d1aea8683c ====
