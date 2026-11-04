// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/shell/dialog_windows.go
// About: File and folder pickers (common item dialogs) and opening a file as Explorer would.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// File and folder pickers (common item dialogs) and opening a file as Explorer would.
//
// The pickers are the COM objects FileOpenDialog and FileSaveDialog, driven through their
// IFileDialog interface by vtable index; the folder picker is the open dialog with
// FOS_PICKFOLDERS. Callers: the maker's window (Browse, Add file, Add folder, Open, Save) and the
// setup wizard's folder page; Open runs the last built setup.exe and the finish page's program.
//
// Flow of one dialog (fileDialog):
//  1. Start COM on this thread and create the open or save dialog.
//  2. Add options to the dialog's defaults, then set the title, file types, default extension,
//     file name and starting folder.
//  3. Show it modally over owner; cancelling returns "" without error.
//  4. Take the chosen item and ask it for its file-system path.

package shell

import (
	"os"
	"path/filepath"
	"unsafe"
)

// Class and interface ids (shobjidl.h) of the dialogs and of IShellItem, and the shell32 functions
// used here.
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
//
// IFileDialog derives from IModalWindow, whose Show is index 3 right after IUnknown's three; the
// IFileDialog methods follow in shobjidl.h order (SetFileTypes 4 ... SetDefaultExtension 22), and
// IShellItem's GetDisplayName is its third own method (5). The rest are flag values:
// SIGDN_FILESYSPATH asks for a plain file-system path, errCancelled is
// HRESULT_FROM_WIN32(ERROR_CANCELLED), which Show returns when the user cancels, and the fos*
// values are FILEOPENDIALOGOPTIONS bits.
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

// filterSpec is the Win32 COMDLG_FILTERSPEC: a display name and a pattern such as "*.txt;*.md",
// both UTF-16, as SetFileTypes takes them in an array.
type filterSpec struct{ name, spec *uint16 }

// PickFolder asks for a folder, starting near start; "" if cancelled.
//
// owner is the window the dialog is modal to (0 for none). start need not exist: the dialog opens
// in the nearest folder above it that does.
func PickFolder(owner uintptr, title, start string) (string, error) {
	return fileDialog(owner, dialogArgs{title: title, start: start, folder: true})
}

// OpenFile asks for an existing file; "" if cancelled.
//
// filters are the file-type choices, the first one selected; none means every file shows.
func OpenFile(owner uintptr, title, start string, filters []Filter) (string, error) {
	return fileDialog(owner, dialogArgs{title: title, start: start, filters: filters})
}

// SaveFile asks where to save a file (asking before replacing one); "" if cancelled.
//
// name is the suggested file name and ext the extension (without the dot) added when the user types
// a name without one.
func SaveFile(owner uintptr, title, start, name, ext string, filters []Filter) (string, error) {
	return fileDialog(owner, dialogArgs{title: title, start: start, name: name, ext: ext, filters: filters, save: true})
}

// dialogArgs is what one dialog shows: its title, the starting folder, the suggested name and
// default extension (save only), the file types, and whether it picks a folder or a save target.
type dialogArgs struct {
	title, start, name, ext string
	filters                 []Filter
	folder, save            bool
}

// fileDialog shows one open, save or folder dialog modal to owner and returns the chosen path, or
// "" when cancelled.
//
// It blocks until the dialog closes, running the dialog's own message loop on a locked OS thread.
// Errors are COM failures as HRESULTs; the results of the setter calls are not checked.
//
// Steps:
//  1. Create FileSaveDialog for a save, else FileOpenDialog.
//  2. Read the default options and add FOS_FORCEFILESYSTEM (only real files, no virtual folders)
//     and FOS_NOCHANGEDIR (leave the process's working folder alone), then the mode's own: pick
//     folders; ask before overwriting; or require an existing path and file.
//  3. Set the title, file types, default extension and file name when given, and the starting
//     folder when one exists.
//  4. Show the dialog; ERROR_CANCELLED means no choice.
//  5. Get the result item and its file-system path, freeing the shell's string.
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
		// SetFolder (not SetDefaultFolder) opens here even when the dialog remembers a last folder
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
//
// It walks up with filepath.Dir until a folder exists, stopping at the volume root, and makes the
// item with SHCreateItemFromParsingName. nil when dir is "", nothing on the way exists, or the item
// cannot be made; otherwise the caller releases it.
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
//
// It calls ShellExecuteW with the "open" verb, args as the command line and dir as the working
// folder, shown normally, and does not wait. ShellExecuteW reports success with a value above 32
// (smaller values are old-style error codes); on failure the error is the thread's last error,
// which the call sets.
func Open(file, args, dir string) error {
	r, _, err := procShellExecute.Call(0, uintptr(unsafe.Pointer(utf16("open"))), uintptr(unsafe.Pointer(utf16(file))),
		uintptr(unsafe.Pointer(utf16(args))), uintptr(unsafe.Pointer(utf16(dir))), 1) // SW_SHOWNORMAL
	if r <= 32 {
		return err
	}
	return nil
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=c3e84017ccdccb92ba31c53b7e5963a1ee672ecd232786613aba7c1ae44ee394 ====
