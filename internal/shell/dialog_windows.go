// ==== ACME Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/shell/dialog_windows.go
// About: Part of package shell in install_wizard_maker.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

package shell

import (
	"os"
	"path/filepath"
	"unsafe"
)

var (
	clsidFileOpenDialog = MustGUID("{DC1C5A9C-E88A-4DDE-A5A1-60F82A20AEF7}")
	clsidFileSaveDialog = MustGUID("{C0B4E2F3-BA21-4773-8DBA-335EC946EB8B}")
	iidFileOpenDialog   = MustGUID("{D57C7288-D4AD-4768-BE02-9D969532D960}")
	iidFileSaveDialog   = MustGUID("{84BCCD23-5FDE-4CDB-AEA4-AF64B83D78AB}")
	iidShellItem        = MustGUID("{43826D1E-E718-42EE-BC55-A1E261C37BFE}")

	procSHCreateItemFromParsingName = shell32.NewProc("SHCreateItemFromParsingName")
	procShellExecute                = shell32.NewProc("ShellExecuteW")
)

// IFileDialog and IShellItem methods used here, by vtable index.
const (
	dialogShow          = 3
	dialogSetFileTypes  = 4
	dialogSetOptions    = 9
	dialogGetOptions    = 10
	dialogSetFolder     = 12
	dialogSetFileName   = 15
	dialogSetTitle      = 17
	dialogGetResult     = 20
	dialogSetDefaultExt = 22
	itemGetDisplayName  = 5
	sigdnFileSysPath    = 0x80058000
	errCancelled        = 0x800704C7
	fosOverwritePrompt  = 0x2
	fosNoChangeDir      = 0x8
	fosPickFolders      = 0x20
	fosForceFileSystem  = 0x40
	fosPathMustExist    = 0x800
	fosFileMustExist    = 0x1000
)

// Filter is one choice of file type in a file dialog, e.g. {"Icons", "*.ico"}.
type Filter struct{ Name, Pattern string }

type filterSpec struct{ name, spec *uint16 }

// PickFolder asks for a folder, starting near start; "" if cancelled.
func PickFolder(owner uintptr, title, start string) (string, error) {
	return fileDialog(owner, dialogArgs{title: title, start: start, folder: true})
}

// OpenFile asks for an existing file; "" if cancelled.
func OpenFile(owner uintptr, title, start string, filters []Filter) (string, error) {
	return fileDialog(owner, dialogArgs{title: title, start: start, filters: filters})
}

// SaveFile asks where to save a file (asking before replacing one); "" if cancelled.
func SaveFile(owner uintptr, title, start, name, ext string, filters []Filter) (string, error) {
	return fileDialog(owner, dialogArgs{title: title, start: start, name: name, ext: ext, filters: filters, save: true})
}

type dialogArgs struct {
	title, start, name, ext string
	filters                 []Filter
	folder, save            bool
}

func fileDialog(owner uintptr, a dialogArgs) (string, error) {
	var path string
	err := withCOM(func() error {
		clsid, iid := &clsidFileOpenDialog, &iidFileOpenDialog
		if a.save {
			clsid, iid = &clsidFileSaveDialog, &iidFileSaveDialog
		}
		d, err := create(clsid, iid)
		if err != nil {
			return err
		}
		defer d.release()
		var opts uint32
		d.call(dialogGetOptions, uintptr(unsafe.Pointer(&opts)))
		opts |= fosForceFileSystem | fosNoChangeDir
		switch {
		case a.folder:
			opts |= fosPickFolders
		case a.save:
			opts |= fosOverwritePrompt
		default:
			opts |= fosPathMustExist | fosFileMustExist
		}
		d.call(dialogSetOptions, uintptr(opts))
		if a.title != "" {
			d.call(dialogSetTitle, uintptr(unsafe.Pointer(utf16(a.title))))
		}
		if len(a.filters) > 0 {
			specs := make([]filterSpec, len(a.filters))
			for i, f := range a.filters {
				specs[i] = filterSpec{utf16(f.Name), utf16(f.Pattern)}
			}
			d.call(dialogSetFileTypes, uintptr(len(specs)), uintptr(unsafe.Pointer(&specs[0])))
		}
		if a.ext != "" {
			d.call(dialogSetDefaultExt, uintptr(unsafe.Pointer(utf16(a.ext))))
		}
		if a.name != "" {
			d.call(dialogSetFileName, uintptr(unsafe.Pointer(utf16(a.name))))
		}
		if item := shellItem(a.start); item != nil {
			d.call(dialogSetFolder, uintptr(unsafe.Pointer(item)))
			item.release()
		}
		hr := d.call(dialogShow, owner)
		if uint32(hr) == errCancelled {
			return nil
		}
		if hr < 0 {
			return hrError("IFileDialog.Show", hr)
		}
		var item *object
		if hr := d.call(dialogGetResult, uintptr(unsafe.Pointer(&item))); hr < 0 {
			return hrError("IFileDialog.GetResult", hr)
		}
		defer item.release()
		var p *uint16
		if hr := item.call(itemGetDisplayName, sigdnFileSysPath, uintptr(unsafe.Pointer(&p))); hr < 0 {
			return hrError("IShellItem.GetDisplayName", hr)
		}
		path = fromTaskMem(p)
		return nil
	})
	return path, err
}

// shellItem is the nearest existing folder at or above dir, as a shell item.
func shellItem(dir string) *object {
	for d := dir; d != ""; d = filepath.Dir(d) {
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			var item *object
			if r, _, _ := procSHCreateItemFromParsingName.Call(uintptr(unsafe.Pointer(utf16(d))), 0,
				uintptr(unsafe.Pointer(&iidShellItem)), uintptr(unsafe.Pointer(&item))); int32(r) >= 0 {
				return item
			}
			return nil
		}
		if filepath.Dir(d) == d {
			break
		}
	}
	return nil
}

// Open opens a file (a program, a shortcut, a document) the way Explorer would.
func Open(file, args, dir string) error {
	r, _, err := procShellExecute.Call(0, uintptr(unsafe.Pointer(utf16("open"))), uintptr(unsafe.Pointer(utf16(file))),
		uintptr(unsafe.Pointer(utf16(args))), uintptr(unsafe.Pointer(utf16(dir))), 1) // SW_SHOWNORMAL
	if r <= 32 {
		return err
	}
	return nil
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=675d6ebb92d0262d383790ecc5182da329136fe2acc82bc47f460280a47427ab ====
