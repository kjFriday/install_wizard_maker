// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/install/apply.go
// About: Apply carries out a Plan as one transaction: it completes, or leaves everything as it was.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Apply carries out a Plan as one transaction: it completes, or leaves everything as it was.
//
// The wizard calls Apply after MakePlan with an Env of real helpers (shell.MakeLinkDir writes the
// .lnk files, the setup's payload is the Source); the tests pass fakes and a Fault hook.
//
// How the transaction works, for a reader who has not met one built on a file system:
//   - Nothing outside <dir>\.setup changes until every packed file has been unpacked into .setup\new
//     and its size and SHA-256 checked.
//   - Each change after that pushes its inverse on an undo stack. Files move by rename: a replaced
//     file goes to .setup\old and is renamed back, a new file is simply removed again. An old
//     shortcut goes to .setup\old\.links, and the registry key is written back as it was.
//   - On any error, or a Cancel, the stack runs last-in first-out, which restores the folder exactly,
//     and the folders setup made are removed.
//   - On success the stack is dropped and .setup is deleted, taking the old files with it: that
//     delete is the commit. A crash leaves .setup behind, and the next setup refuses to run until
//     the user has looked at it, since .setup\old may hold the only copy of a file.
//
// Start reading at Apply, then tx.run, whose numbered phases are the transaction.

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
// CopyChecked writes the entry name ("files/<path>") to w and fails unless exactly size bytes with
// the hex SHA-256 sum arrived; *payload.File implements it.
type Source interface {
	CopyChecked(name string, w io.Writer, size int64, sum string) error
}

// Env is what an install needs from the outside; tests pass fakes.
// Progress, Cancelled and Fault may be nil. All of them are called on Apply's own goroutine.
type Env struct {
	Places      Places
	Registry    Registry
	Link        func(Shortcut) error // writes a .lnk file
	Source      Source
	Icon        []byte                               // app.ico
	Uninstaller []byte                               // uninstall.exe
	Progress    func(done, total int64, file string) // bytes unpacked so far of all packed files; file "" when unpacking ends
	Cancelled   func() bool                          // polled before each file is unpacked and once after; true rolls back
	Fault       func(step string) error              // tests: fail on purpose at a step
	Retry       time.Duration                        // how long a locked file is retried (default 2 s)
}

// ErrCancelled: the user cancelled, and everything was put back.
var ErrCancelled = errors.New("setup was cancelled; nothing was changed")

// Result says what an install or uninstall left alone.
type Result struct {
	Kept      []string // files left in place: changed since installed, or not installed by this setup
	AfterExit []string // uninstall: the running uninstaller and the folders to remove once it has exited
}

// tx is one Apply in progress: the plan, the staging folders and the undo stack.
// It lives for one call and is not shared between goroutines.
type tx struct {
	env      Env
	plan     *Plan
	newDir   string         // <dir>\.setup\new: unpacked files wait here
	oldDir   string         // <dir>\.setup\old: replaced and removed files wait here
	undo     []func() error // each step's inverse, run last-in first-out on failure
	madeDirs []string       // folders this setup created, outermost first, removed on failure
	done     int64          // bytes unpacked so far, for Progress
	total    int64          // bytes to unpack in all
	backups  int            // shortcuts set aside so far, which names .links\<n>.lnk
}

// Apply carries out a plan as one transaction (see the package comment).
// It returns what it left alone in Result.Kept. On failure everything is put back and the error is a
// *Refusal (a preflight check failed), ErrCancelled, or what went wrong. It may change p.Marker
// (it adds the icon's and uninstaller's hashes).
//
// Steps:
//  1. Default Retry to 2 s.
//  2. preflight: refuse before touching anything.
//  3. run the phases; on error, run the undo stack.
//  4. Remove .setup in every case: after a rollback it holds only copies, after success this is the
//     commit.
//  5. On error, remove the folders setup made (only those now empty go).
//
// Known limitation: the rollback is best effort. If an undo step fails (a file locked at that very
// moment), step 4 still deletes .setup, and with it any original that was not moved back.
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
		t.rollback() // must come first: it moves the originals back out of .setup\old
	}
	os.RemoveAll(filepath.Join(p.Dir, setupDir))
	if err != nil {
		t.removeMadeDirs()
		return Result{}, err
	}
	return res, nil
}

// preflight refuses, before anything changes, an install that could not finish cleanly:
//   - a .setup folder left by a setup that crashed (it may hold the only copy of a file);
//   - a folder where a file must go;
//   - a file the old marker does not list (the user's, which setup must not replace or remove);
//   - a file that is open in another program or read-only.
//
// It checks every packed file, the three files setup writes, and the files Remove may delete.
// All but the first are refusals with Retry set: the user can fix them and try again.
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
// The returned func takes a folder-relative "/" path and compares case-insensitively. With no old
// install nothing is owned, so any existing file is in the way.
func ownedBy(old *Marker, dir string) func(rel string) bool {
	files, folders := map[string]bool{}, []string{}
	if old != nil {
		for _, raw := range old.Files {
			e, ok := entry(raw)
			if !ok {
				continue
			}
			// the Rust sibling lists whole folders ("www"): everything under one counts as owned,
			// matched by the "name/" prefix
			if info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(e))); err == nil && info.IsDir() {
				folders = append(folders, strings.ToLower(e)+"/")
			} else {
				files[strings.ToLower(e)] = true
			}
		}
	}
	return func(rel string) bool {
		low := strings.ToLower(rel)
		// the three files setup itself writes belong to any earlier install, listed or not
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

// inUse reports whether path exists but cannot be opened for writing: another program holds it
// without write sharing (a running exe), or it is read-only. Opening, not renaming, tests this
// without changing anything.
func inUse(path string) bool {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err == nil {
		f.Close()
		return false
	}
	return !errors.Is(err, fs.ErrNotExist)
}

// fault asks the test hook Env.Fault whether to fail at step; nil when there is no hook.
// The step names are a contract with TestApplyRollsBackAtEveryStep: "stage:<path>", "move:<path>",
// "remove:<path>", "link:<n>" (1-based) and "registry".
func (t *tx) fault(step string) error {
	if t.env.Fault != nil {
		return t.env.Fault(step)
	}
	return nil
}

// cancelled returns ErrCancelled once Env.Cancelled reports true.
func (t *tx) cancelled() error {
	if t.env.Cancelled != nil && t.env.Cancelled() {
		return ErrCancelled
	}
	return nil
}

// progress reports the bytes unpacked so far and the file about to be unpacked.
func (t *tx) progress(file string) {
	if t.env.Progress != nil {
		t.env.Progress(t.done, t.total, file)
	}
}

// run is the transaction's body. Every change it makes pushes its inverse on t.undo, so Apply can
// roll back from an error at any point. Cancel is honoured only while unpacking: once files start
// moving into place the install runs to the end (or to an error).
//
// Steps:
//  1. Unpack and check every packed file into .setup\new, then the icon, the uninstaller and the
//     marker. Nothing outside .setup has changed yet.
//  2. Swap each file into place, the marker last, so the new marker never lists a file that is not
//     in place yet.
//  3. Set aside the files this version drops, if still unchanged; report the rest as Kept.
//  4. Set aside old shortcuts and write the new ones.
//  5. Write the Apps & features entry.
//
// After step 5 the install is complete; Apply's removal of .setup commits it.
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
	// the files setup writes itself get hashes too, so an uninstall can tell they are unchanged;
	// the marker must be complete before it is staged. (Ranging over the map stages these three in
	// random order, which does not matter: each is its own file.)
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
		// the old marker may key the hash with "/" or with "\"; the hash is checked again here
		// because the user may have edited the file since MakePlan ran
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
		// a shortcut already there (from the old version, or the user's) is set aside, not overwritten
		if err := t.setAside(l.Path, t.linkBackup()); err != nil {
			return res, err
		}
		if err := t.mkdirs(filepath.Dir(l.Path)); err != nil {
			return res, err
		}
		if err := t.env.Link(l); err != nil {
			os.Remove(l.Path) // a half-written .lnk is not on the undo stack yet
			return res, fmt.Errorf("shortcut %s: %w", l.Path, err)
		}
		path := l.Path
		t.undo = append(t.undo, func() error { return os.Remove(path) })
		if err := t.fault("link:" + strconv.Itoa(i+1)); err != nil {
			return res, err
		}
	}

	// 5. the Apps & features entry, last because a registry write cannot be staged: the old values
	// are read first so restore can put them back, or delete the key when there was none
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
	t.undo = append(t.undo, restore) // pushed only now: a failed write was restored just above
	if err := t.fault("registry"); err != nil {
		return res, err
	}

	// committed: what was set aside goes with .setup; tidy up folders the removals emptied
	// (removed shortcuts tidy only inside the Start menu: the desktop folder itself must stay)
	for _, rel := range p.Remove {
		removeEmptyParents(filepath.Join(p.Dir, filepath.FromSlash(rel)), p.Dir)
	}
	for _, s := range p.Unlink {
		removeEmptyParents(s, t.env.Places.StartMenu)
	}
	return res, nil
}

// stage writes one file under .setup\new. rel is the folder-relative "/" path; write fills the
// file. A failed write or close is the error; the partial file stays in .setup, which Apply deletes.
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
// Both are renames within one folder tree on one volume, so each is atomic and costs no copy.
// Undo order: the new file is removed first, then the old one is renamed back.
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
// A missing path is not an error and pushes nothing. backup's folder is created inside .setup\old,
// which goes away with .setup, so it is not recorded in madeDirs.
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

// linkBackup names the next backup slot for a shortcut: .setup\old\.links\1.lnk, 2.lnk, ...
// Shortcuts live outside the install folder, so they get numbered names rather than mirrored paths.
func (t *tx) linkBackup() string {
	t.backups++
	return filepath.Join(t.oldDir, ".links", strconv.Itoa(t.backups)+".lnk")
}

// rename retries for a moment: antivirus scanners briefly lock files that were just written.
// It tries every 50 ms until Env.Retry has passed, and only for the errors in retryable; any other
// error (or the last one) is returned.
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

// The Windows errors a rename gets while another program briefly holds the file.
// The numbers are Win32 system error codes (winerror.h); os.Rename wraps them in *os.LinkError.
const (
	errAccessDenied     = syscall.Errno(5)  // ERROR_ACCESS_DENIED
	errSharingViolation = syscall.Errno(32) // ERROR_SHARING_VIOLATION
	errLockViolation    = syscall.Errno(33) // ERROR_LOCK_VIOLATION
)

// retryable reports whether err is one of those passing lock errors (errors.As looks through the
// *os.LinkError to the syscall.Errno).
func retryable(err error) bool {
	var errno syscall.Errno
	return errors.As(err, &errno) && (errno == errAccessDenied || errno == errSharingViolation || errno == errLockViolation)
}

// mkdirs makes a folder and its parents, remembering which it made so a rollback removes them.
// It walks up from dir until a folder exists (or the volume root), then appends the missing ones to
// madeDirs outermost first, so removeMadeDirs, reading backwards, removes the innermost first.
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

// rollback runs the undo stack last-in first-out, so each step is undone in the state it left.
func (t *tx) rollback() {
	for i := len(t.undo) - 1; i >= 0; i-- {
		t.undo[i]() // best effort: keep putting back the rest
	}
}

// removeMadeDirs removes the folders this setup created, innermost first; any that still hold
// something (a kept file, someone else's) stay.
func (t *tx) removeMadeDirs() {
	for i := len(t.madeDirs) - 1; i >= 0; i-- {
		os.Remove(t.madeDirs[i]) // only succeeds when empty
	}
}

// removeEmptyParents removes path's folder and its parents while they are empty, up to (not) stop.
// os.Remove fails on a folder that is not empty, which ends the walk; within keeps it inside stop.
func removeEmptyParents(path, stop string) {
	for d := filepath.Dir(path); within(stop, d); d = filepath.Dir(d) {
		if os.Remove(d) != nil {
			return
		}
	}
}

// sum is the lowercase hex SHA-256 of b, the form the marker and the manifest store.
func sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// fileSum is the lowercase hex SHA-256 of the file at path, streamed rather than read whole. A
// missing file's error matches fs.ErrNotExist.
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
// SetFileAttributes replaces all attributes; failure is ignored, since hiding is cosmetic.
func hide(path string) {
	if p, err := syscall.UTF16PtrFromString(path); err == nil {
		syscall.SetFileAttributes(p, syscall.FILE_ATTRIBUTE_HIDDEN)
	}
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=12d77f7dd94695b6997a890f127a9eca9a90c9f1ccc99588d76e37541263c75e ====
