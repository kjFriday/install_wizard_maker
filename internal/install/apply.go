// ==== Generic Company Inc. portfolio ====================================
// Project: install_wizard_maker | File: internal/install/apply.go
// About: ==== Generic Company Inc. portfolio ===============================================...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// ==== Generic Company Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/install/apply.go
// About: Part of package install in install_wizard_maker.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

package install

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Source is where the packed files come from (a setup.exe's payload).
type Source interface {
	CopyChecked(name string, w io.Writer, size int64, sum string) error
}

// Env is what an install needs from the outside; tests pass fakes.
type Env struct {
	Places      Places
	Registry    Registry
	Link        func(Shortcut) error // writes a .lnk file
	Source      Source
	Icon        []byte // app.ico
	Uninstaller []byte // uninstall.exe
	Progress    func(done, total int64, file string)
	Cancelled   func() bool
	Fault       func(step string) error // tests: fail on purpose at a step
	Retry       time.Duration           // how long a locked file is retried (default 2 s)
}

// ErrCancelled: the user cancelled, and everything was put back.
var ErrCancelled = errors.New("setup was cancelled; nothing was changed")

// Result says what an install or uninstall left alone.
type Result struct {
	Kept      []string // files left in place: changed since installed, or not installed by this setup
	AfterExit []string // uninstall: the running uninstaller and the folders to remove once it has exited
}

type tx struct {
	env      Env
	plan     *Plan
	newDir   string
	oldDir   string
	undo     []func() error
	madeDirs []string
	done     int64
	total    int64
	backups  int
}

// Apply carries out a plan as one transaction (see the package comment).
func Apply(p *Plan, env Env) (Result, error) {
	if env.Retry == 0 {
		env.Retry = 2 * time.Second
	}
	t := &tx{env: env, plan: p, newDir: filepath.Join(p.Dir, setupDir, "new"), oldDir: filepath.Join(p.Dir, setupDir, "old")}
	if err := t.preflight(); err != nil {
		return Result{}, err
	}
	res, err := t.run()
	if err != nil {
		t.rollback()
	}
	os.RemoveAll(filepath.Join(p.Dir, setupDir))
	if err != nil {
		t.removeMadeDirs()
		return Result{}, err
	}
	return res, nil
}

func (t *tx) preflight() error {
	p := t.plan
	if _, err := os.Stat(filepath.Join(p.Dir, setupDir)); err == nil {
		return refuse(false, "An earlier setup in %s did not finish. Look in its %s folder for the files it set aside, then remove that folder.", p.Dir, setupDir)
	}
	var check []string
	for _, f := range p.M.Files {
		check = append(check, f.Path)
	}
	check = append(append(check, IconName, UninstallerName, MarkerName), p.Remove...)
	owned := ownedBy(p.Old, p.Dir)
	for _, rel := range check {
		target := filepath.Join(p.Dir, filepath.FromSlash(rel))
		info, err := os.Lstat(target)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.IsDir() {
			return refuse(true, "%s is a folder where setup needs to put a file. Move it and click Retry.", target)
		}
		if !owned(rel) {
			return refuse(true, "%s is in the way: setup only replaces files it installed. Move it and click Retry.", target)
		}
		if inUse(target) {
			return refuse(true, "%s is in use or read-only. Close %s and click Retry.", target, p.M.Name)
		}
	}
	return nil
}

// ownedBy: which existing files an earlier install (old) put there, so setup may replace them.
func ownedBy(old *Marker, dir string) func(rel string) bool {
	files, folders := map[string]bool{}, []string{}
	if old != nil {
		for _, raw := range old.Files {
			e, ok := entry(raw)
			if !ok {
				continue
			}
			if info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(e))); err == nil && info.IsDir() {
				folders = append(folders, strings.ToLower(e)+"/")
			} else {
				files[strings.ToLower(e)] = true
			}
		}
	}
	return func(rel string) bool {
		low := strings.ToLower(rel)
		if old != nil && (low == strings.ToLower(IconName) || low == strings.ToLower(UninstallerName) || low == strings.ToLower(MarkerName)) {
			return true
		}
		if files[low] {
			return true
		}
		for _, f := range folders {
			if strings.HasPrefix(low, f) {
				return true
			}
		}
		return false
	}
}

func inUse(path string) bool {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err == nil {
		f.Close()
		return false
	}
	return !errors.Is(err, fs.ErrNotExist)
}

func (t *tx) fault(step string) error {
	if t.env.Fault != nil {
		return t.env.Fault(step)
	}
	return nil
}

func (t *tx) cancelled() error {
	if t.env.Cancelled != nil && t.env.Cancelled() {
		return ErrCancelled
	}
	return nil
}

func (t *tx) progress(file string) {
	if t.env.Progress != nil {
		t.env.Progress(t.done, t.total, file)
	}
}

func (t *tx) run() (Result, error) {
	p := t.plan
	var res Result
	if err := t.mkdirs(p.Dir); err != nil {
		return res, err
	}
	for _, d := range []string{t.newDir, t.oldDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return res, err
		}
	}
	hide(filepath.Join(p.Dir, setupDir))
	for _, f := range p.M.Files {
		t.total += f.Size
	}

	// 1. unpack and check everything first: nothing outside .setup has changed yet
	for _, f := range p.M.Files {
		if err := t.cancelled(); err != nil {
			return res, err
		}
		t.progress(f.Path)
		if err := t.stage(f.Path, func(w io.Writer) error {
			return t.env.Source.CopyChecked("files/"+f.Path, w, f.Size, f.SHA256)
		}); err != nil {
			return res, err
		}
		t.done += f.Size
		if err := t.fault("stage:" + f.Path); err != nil {
			return res, err
		}
	}
	t.progress("")
	p.Marker.SHA256[IconName] = sum(t.env.Icon)
	p.Marker.SHA256[UninstallerName] = sum(t.env.Uninstaller)
	for name, data := range map[string][]byte{IconName: t.env.Icon, UninstallerName: t.env.Uninstaller, MarkerName: p.Marker.JSON()} {
		if err := t.stage(name, func(w io.Writer) error { _, err := w.Write(data); return err }); err != nil {
			return res, err
		}
	}
	if err := t.cancelled(); err != nil {
		return res, err
	}

	// 2. swap the files in, the marker last
	order := make([]string, 0, len(p.M.Files)+3)
	for _, f := range p.M.Files {
		order = append(order, f.Path)
	}
	order = append(order, IconName, UninstallerName, MarkerName)
	for _, rel := range order {
		if err := t.swap(rel); err != nil {
			return res, err
		}
		if err := t.fault("move:" + rel); err != nil {
			return res, err
		}
	}

	// 3. files the new version dropped: set aside if unchanged, else keep
	for _, rel := range p.Remove {
		target := filepath.Join(p.Dir, filepath.FromSlash(rel))
		want := p.Old.SHA256[rel]
		if want == "" {
			want = p.Old.SHA256[filepath.FromSlash(rel)]
		}
		got, err := fileSum(target)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil || got != want {
			res.Kept = append(res.Kept, rel)
			continue
		}
		if err := t.setAside(target, filepath.Join(t.oldDir, filepath.FromSlash(rel))); err != nil {
			return res, err
		}
		if err := t.fault("remove:" + rel); err != nil {
			return res, err
		}
	}
	res.Kept = append(res.Kept, p.Kept...)

	// 4. shortcuts: old ones set aside (so they can come back), new ones written
	for _, s := range p.Unlink {
		if err := t.setAside(s, t.linkBackup()); err != nil {
			return res, err
		}
	}
	for i, l := range p.Links {
		if err := t.setAside(l.Path, t.linkBackup()); err != nil {
			return res, err
		}
		if err := t.mkdirs(filepath.Dir(l.Path)); err != nil {
			return res, err
		}
		if err := t.env.Link(l); err != nil {
			os.Remove(l.Path)
			return res, fmt.Errorf("shortcut %s: %w", l.Path, err)
		}
		path := l.Path
		t.undo = append(t.undo, func() error { return os.Remove(path) })
		if err := t.fault("link:" + strconv.Itoa(i+1)); err != nil {
			return res, err
		}
	}

	// 5. the Apps & features entry
	prev, found, err := t.env.Registry.Read(p.M.ID)
	if err != nil {
		return res, err
	}
	restore := func() error {
		if !found {
			return t.env.Registry.Delete(p.M.ID)
		}
		return t.env.Registry.Write(p.M.ID, prev)
	}
	if err := t.env.Registry.Write(p.M.ID, p.Registry); err != nil {
		restore() // a half-written key is put back as it was
		return res, fmt.Errorf("registry: %w", err)
	}
	t.undo = append(t.undo, restore)
	if err := t.fault("registry"); err != nil {
		return res, err
	}

	// committed: what was set aside goes with .setup; tidy up folders the removals emptied
	for _, rel := range p.Remove {
		removeEmptyParents(filepath.Join(p.Dir, filepath.FromSlash(rel)), p.Dir)
	}
	for _, s := range p.Unlink {
		removeEmptyParents(s, t.env.Places.StartMenu)
	}
	return res, nil
}

// stage writes one file under .setup\new.
func (t *tx) stage(rel string, write func(io.Writer) error) error {
	path := filepath.Join(t.newDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	err = write(f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// swap moves the staged file into place, setting aside the one it replaces.
func (t *tx) swap(rel string) error {
	target := filepath.Join(t.plan.Dir, filepath.FromSlash(rel))
	if err := t.mkdirs(filepath.Dir(target)); err != nil {
		return err
	}
	if err := t.setAside(target, filepath.Join(t.oldDir, filepath.FromSlash(rel))); err != nil {
		return err
	}
	if err := t.rename(filepath.Join(t.newDir, filepath.FromSlash(rel)), target); err != nil {
		return err
	}
	t.undo = append(t.undo, func() error { return os.Remove(target) })
	return nil
}

// setAside moves path (if it exists) to backup, to be put back on rollback.
func (t *tx) setAside(path, backup string) error {
	if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
		return err
	}
	if err := t.rename(path, backup); err != nil {
		return err
	}
	t.undo = append(t.undo, func() error { return t.rename(backup, path) })
	return nil
}

func (t *tx) linkBackup() string {
	t.backups++
	return filepath.Join(t.oldDir, ".links", strconv.Itoa(t.backups)+".lnk")
}

// rename retries for a moment: antivirus scanners briefly lock files that were just written.
func (t *tx) rename(from, to string) error {
	deadline := time.Now().Add(t.env.Retry)
	for {
		err := os.Rename(from, to)
		if err == nil || !retryable(err) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func retryable(err error) bool {
	var errno syscall.Errno
	return errors.As(err, &errno) && (errno == 5 || errno == 32 || errno == 33) // access denied, sharing, lock
}

// mkdirs makes a folder and its parents, remembering which it made so a rollback removes them.
func (t *tx) mkdirs(dir string) error {
	var missing []string
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(d); err == nil {
			break
		}
		missing = append(missing, d)
		if filepath.Dir(d) == d {
			break
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for i := len(missing) - 1; i >= 0; i-- {
		t.madeDirs = append(t.madeDirs, missing[i])
	}
	return nil
}

func (t *tx) rollback() {
	for i := len(t.undo) - 1; i >= 0; i-- {
		t.undo[i]() // best effort: keep putting back the rest
	}
}

func (t *tx) removeMadeDirs() {
	for i := len(t.madeDirs) - 1; i >= 0; i-- {
		os.Remove(t.madeDirs[i]) // only succeeds when empty
	}
}

// removeEmptyParents removes path's folder and its parents while they are empty, up to (not) stop.
func removeEmptyParents(path, stop string) {
	for d := filepath.Dir(path); within(stop, d); d = filepath.Dir(d) {
		if os.Remove(d) != nil {
			return
		}
	}
}

func sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func fileSum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// hide marks .setup hidden, so a crash leaves no clutter in plain view.
func hide(path string) {
	if p, err := syscall.UTF16PtrFromString(path); err == nil {
		syscall.SetFileAttributes(p, syscall.FILE_ATTRIBUTE_HIDDEN)
	}
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=b59b32b8d091d52627034cfd0c198a661c9a20d6edb88672f3ed7c012a52c724 ====
