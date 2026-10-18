package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"acme/wizard/internal/app"
	"acme/wizard/internal/install"
	"acme/wizard/internal/maker"
	"acme/wizard/internal/shell"
	"acme/wizard/internal/spec"
	"acme/wizard/internal/testexe"
)

func TestMain(m *testing.M) {
	code := m.Run()
	testexe.Cleanup()
	os.Exit(code)
}

// runExe starts an exe and returns its output and exit code.
func runExe(t *testing.T, exe string, args ...string) (string, int) {
	t.Helper()
	out, err := exec.Command(exe, args...).CombinedOutput()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return string(out), ee.ExitCode()
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(out), 0
}

// build makes the fixture's setup.exe, after edit has changed its setup.json.
func build(t *testing.T, dir string, edit func(map[string]any)) string {
	t.Helper()
	file := filepath.Join(dir, "setup.json")
	if edit != nil {
		var s map[string]any
		b, _ := os.ReadFile(file)
		json.Unmarshal(b, &s)
		edit(s)
		b, _ = json.MarshalIndent(s, "", "  ")
		os.WriteFile(file, b, 0o644)
	}
	out := filepath.Join(t.TempDir(), "setup.exe")
	if text, code := runExe(t, testexe.Maker(t), "build", "-o", out, file); code != 0 {
		t.Fatalf("build: %d %s", code, text)
	}
	return out
}

func registry(t *testing.T, root string) map[string]map[string]any {
	t.Helper()
	var data map[string]map[string]any
	b, err := os.ReadFile(filepath.Join(root, "registry.json"))
	if err != nil {
		return nil
	}
	if err := json.Unmarshal(b, &data); err != nil {
		t.Fatal(err)
	}
	return data
}

const key = `HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\ACME.WizardFixture`

func files(t *testing.T, dir string) []string {
	var out []string
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	return out
}

func gone(path string, within time.Duration) bool {
	for deadline := time.Now().Add(within); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			return true
		}
	}
	return false
}

func TestQuietSetupEndToEnd(t *testing.T) {
	fixture := testexe.Fixture(t)
	setup := build(t, fixture, nil)
	root := filepath.Join(t.TempDir(), "sand box & 100%")
	log := filepath.Join(t.TempDir(), "setup.log")
	if out, code := runExe(t, setup, "-quiet", "-root", root, "-log", log); code != 0 {
		t.Fatalf("setup: %d %s", code, out)
	}
	pl := install.SandboxPlaces(root)
	dir := pl.Dir("ACME Inc", "ACME Wizard Fixture")
	if got := files(t, dir); !reflect.DeepEqual(got, []string{"README.md", "app.ico", "data/settings.json", "hello.exe", "install.json", "uninstall.exe"}) {
		t.Fatalf("installed %v", got)
	}
	m, err := install.ReadMarker(dir)
	if err != nil || m.AppID != "ACME.WizardFixture" || len(m.SHA256) != 5 || !strings.HasPrefix(m.Installer, "ACME Wizard Maker") {
		t.Fatalf("marker %+v %v", m, err)
	}
	uninstall := filepath.Join(dir, "uninstall.exe")
	v := registry(t, root)[key]
	if v["UninstallString"] != `"`+uninstall+`" -root "`+root+`"` || v["QuietUninstallString"] != v["UninstallString"].(string)+" -quiet" ||
		v["NoModify"] != float64(1) || v["DisplayName"] != "ACME Wizard Fixture 1.0.0" {
		t.Fatalf("registry %v", v)
	}
	link, err := shell.ReadLink(pl.StartMenuLink("ACME Inc", "ACME Wizard Fixture"))
	if err != nil || link.Target != filepath.Join(dir, "hello.exe") || link.Args != "-version" ||
		link.Icon != filepath.Join(dir, "app.ico") || link.WorkDir != dir {
		t.Fatalf("shortcut %+v %v", link, err)
	}
	if b, _ := os.ReadFile(log); !strings.Contains(string(b), "Installed ACME Wizard Fixture 1.0.0") {
		t.Fatalf("log %s", b)
	}

	// the user changes one file and adds another; uninstall keeps both
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("my notes"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("mine"), 0o644)
	out, code := runExe(t, uninstall, "-quiet", "-root", root)
	if code != 0 || !strings.Contains(out, "README.md") {
		t.Fatalf("uninstall: %d %s", code, out)
	}
	if !gone(uninstall, 15*time.Second) {
		t.Fatal("uninstall.exe still there")
	}
	if got := files(t, dir); !reflect.DeepEqual(got, []string{"README.md", "notes.txt"}) {
		t.Fatalf("left %v", got)
	}
	if registry(t, root)[key] != nil {
		t.Fatal("registry entry left")
	}
	if _, err := os.Stat(filepath.Join(root, "Start Menu", "ACME Inc")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("Start-menu folder left")
	}
}

func TestUpgradeDowngradeAndRefusals(t *testing.T) {
	fixture := testexe.Fixture(t)
	v1 := build(t, fixture, nil)
	v2 := build(t, fixture, func(s map[string]any) {
		s["version"] = "2.0.0"
		s["files"] = []any{map[string]any{"from": "hello.exe"}, map[string]any{"from": "data", "to": "data"}} // no README.md
	})
	root := filepath.Join(t.TempDir(), "root")
	dir := install.SandboxPlaces(root).Dir("ACME Inc", "ACME Wizard Fixture")
	if out, code := runExe(t, v1, "-quiet", "-root", root, "-desktop-shortcut"); code != 0 {
		t.Fatalf("v1: %s", out)
	}
	if out, code := runExe(t, v2, "/S", "-root", root); code != 0 {
		t.Fatalf("v2: %s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "README.md")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("README.md, dropped by v2 and unchanged, should be gone")
	}
	if m, _ := install.ReadMarker(dir); m.Version != "2.0.0" || len(m.Shortcuts) != 2 {
		t.Fatalf("upgrade marker %+v (the desktop shortcut should carry over)", m)
	}
	if out, code := runExe(t, v1, "-quiet", "-root", root); code != 4 || !strings.Contains(out, "already installed") {
		t.Fatalf("downgrade: %d %s", code, out)
	}

	// a folder another program owns is left alone
	other := filepath.Join(t.TempDir(), "other")
	odir := install.SandboxPlaces(other).Dir("ACME Inc", "ACME Wizard Fixture")
	os.MkdirAll(odir, 0o755)
	os.WriteFile(filepath.Join(odir, "install.json"), []byte(`{"app_id": "ACME.Other", "files": []}`), 0o644)
	if out, code := runExe(t, v1, "-quiet", "-root", other); code != 4 {
		t.Fatalf("foreign folder: %d %s", code, out)
	}
	if got := files(t, other); !reflect.DeepEqual(got, []string{"Programs/ACME Inc/ACME Wizard Fixture/install.json"}) {
		t.Fatalf("foreign folder touched: %v", got)
	}

	// a cut-short download says so
	b, _ := os.ReadFile(v1)
	cut := filepath.Join(t.TempDir(), "cut.exe")
	os.WriteFile(cut, b[:len(b)-100], 0o755)
	if out, code := runExe(t, cut, "-quiet"); code != 1 || !strings.Contains(out, "damaged") {
		t.Fatalf("damaged: %d %s", code, out)
	}
}

type smokeReport struct {
	Role    string `json:"role"`
	IconSet bool   `json:"iconSet"`
	Client  [2]int `json:"client"`
	Exit    int    `json:"exit"`
	Panic   string `json:"panic"`
	Pages   []struct {
		Name     string          `json:"name"`
		PNG      string          `json:"png"`
		Flags    map[string]bool `json:"flags"`
		Error    string          `json:"error"`
		Controls []struct {
			Class         string `json:"class"`
			Text          string `json:"text"`
			X, Y, W, H    int
			Visible, Fits bool
		} `json:"controls"`
	} `json:"pages"`
}

// checkSmoke reads a -smoke report and checks every page: captured, in order, and every visible
// control inside the window, clear of the others, its text fitting.
func checkSmoke(t *testing.T, file string, pages []string) smokeReport {
	t.Helper()
	var r smokeReport
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	if r.Panic != "" || !r.IconSet {
		t.Fatalf("%s: panic %q, icon set %v", r.Role, r.Panic, r.IconSet)
	}
	var names []string
	for _, p := range r.Pages {
		names = append(names, p.Name)
		if p.PNG == "" {
			t.Errorf("%s/%s: no capture: %s", r.Role, p.Name, p.Error)
		} else if _, err := os.Stat(filepath.Join(filepath.Dir(file), p.PNG)); err != nil {
			t.Errorf("%s/%s: %v", r.Role, p.Name, err)
		}
		var shown []int
		for i, c := range p.Controls {
			if !c.Visible {
				continue
			}
			if c.X < 0 || c.Y < 0 || c.X+c.W > r.Client[0] || c.Y+c.H > r.Client[1] {
				t.Errorf("%s/%s: %s %q outside the window", r.Role, p.Name, c.Class, c.Text)
			}
			if !c.Fits {
				t.Errorf("%s/%s: %s %q does not fit its %dx%d box", r.Role, p.Name, c.Class, c.Text, c.W, c.H)
			}
			for _, j := range shown {
				o := p.Controls[j]
				if c.X < o.X+o.W && o.X < c.X+c.W && c.Y < o.Y+o.H && o.Y < c.Y+c.H {
					t.Errorf("%s/%s: %q overlaps %q", r.Role, p.Name, c.Text, o.Text)
				}
			}
			shown = append(shown, i)
		}
	}
	if !reflect.DeepEqual(names, pages) {
		t.Fatalf("%s pages %v, want %v", r.Role, names, pages)
	}
	return r
}

// The windows, walked page by page as a user would (-smoke), install and uninstall for real.
func TestWizardWindows(t *testing.T) {
	fixture := testexe.Fixture(t)
	setup := build(t, fixture, nil)
	root := filepath.Join(t.TempDir(), "root")
	shots := filepath.Join(t.TempDir(), "shots")
	if out, code := runExe(t, setup, "-smoke", filepath.Join(shots, "setup.json"), "-root", root); code != 0 {
		t.Fatalf("setup smoke: %d %s", code, out)
	}
	r := checkSmoke(t, filepath.Join(shots, "setup.json"), []string{"welcome", "license", "license-accepted", "folder", "shortcuts", "progress", "finish"})
	if r.Pages[1].Flags["next"] || !r.Pages[2].Flags["next"] {
		t.Fatal("Next should wait for the license to be accepted")
	}
	dir := install.SandboxPlaces(root).Dir("ACME Inc", "ACME Wizard Fixture")
	uninstall := filepath.Join(dir, "uninstall.exe")
	if _, err := os.Stat(uninstall); err != nil {
		t.Fatalf("not installed: %v", err)
	}
	if out, code := runExe(t, uninstall, "-smoke", filepath.Join(shots, "uninstall.json"), "-root", root); code != 0 {
		t.Fatalf("uninstall smoke: %d %s", code, out)
	}
	checkSmoke(t, filepath.Join(shots, "uninstall.json"), []string{"confirm", "removing", "done"})
	if !gone(dir, 15*time.Second) {
		t.Fatalf("left %v", files(t, dir))
	}
}

// A Python program's shortcut runs the newest registered CPython that is new enough.
func TestPythonProgram(t *testing.T) {
	fixture := testexe.Fixture(t)
	python := func(min string) func(map[string]any) {
		return func(s map[string]any) {
			s["run"] = map[string]any{"program": "{python}", "args": []string{`{app}\main.py`}}
			s["requires"] = map[string]any{"python": min}
		}
	}
	root := filepath.Join(t.TempDir(), "root")
	if out, code := runExe(t, build(t, fixture, python("3.99")), "-quiet", "-root", root); code != 4 || !strings.Contains(out, "Python 3.99") {
		t.Fatalf("no such Python: %d %s", code, out)
	}
	out, code := runExe(t, build(t, fixture, python("3.0")), "-quiet", "-root", root)
	if code == 4 && strings.Contains(out, "Python") {
		t.Skip("no Python registered on this computer")
	}
	if code != 0 {
		t.Fatalf("%d %s", code, out)
	}
	pl := install.SandboxPlaces(root)
	dir := pl.Dir("ACME Inc", "ACME Wizard Fixture")
	link, err := shell.ReadLink(pl.StartMenuLink("ACME Inc", "ACME Wizard Fixture"))
	if err != nil || !strings.EqualFold(filepath.Base(link.Target), "pythonw.exe") || link.Args != `"`+filepath.Join(dir, "main.py")+`"` {
		t.Fatalf("%+v %v", link, err)
	}
	if _, err := os.Stat(link.Target); err != nil {
		t.Fatal(err)
	}
}

// The repository's setup.json describes the same program the maker installs itself as.
func TestOwnSetupJSON(t *testing.T) {
	s, err := spec.Load("setup.json")
	if err != nil {
		t.Fatal(err)
	}
	self, err := maker.SelfSpec(os.Args[0], t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != app.ID || s.Name != app.Name || s.Version != app.Version || s.Publisher != app.Publisher || s.Group != app.Group ||
		s.Run.Program != app.ExeName || !reflect.DeepEqual(s.Files, self.Files) || !reflect.DeepEqual(s.Run, self.Run) || s.Shortcuts != self.Shortcuts {
		t.Fatalf("setup.json %+v\nself %+v", s, self)
	}
	if s.Icon != "icon/app.ico" || s.License != "LICENSE" {
		t.Fatalf("icon %q license %q", s.Icon, s.License)
	}
}

// The maker installs and uninstalls itself (rule 18): -install builds its own setup.exe and runs it.
func TestInstallSelf(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	exe := testexe.Maker(t)
	if out, code := runExe(t, exe, "-install", "-quiet", "-root", root); code != 0 {
		t.Fatalf("install: %d %s", code, out)
	}
	dir := install.SandboxPlaces(root).Dir(app.Group, app.Name)
	m, err := install.ReadMarker(dir)
	if err != nil || m == nil || m.AppID != app.ID || m.Version != app.Version {
		t.Fatalf("marker %+v %v", m, err)
	}
	if out, code := runExe(t, filepath.Join(dir, app.ExeName), "-version"); code != 0 || !strings.Contains(out, app.Version) {
		t.Fatalf("installed maker: %d %s", code, out)
	}
	if v := registry(t, root)[`HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\`+app.ID]; v["DisplayName"] != app.Name+" "+app.Version {
		t.Fatalf("registry %v", v)
	}
	if out, code := runExe(t, exe, "-uninstall", "-quiet", "-root", root); code != 0 {
		t.Fatalf("uninstall: %d %s", code, out)
	}
	if !gone(dir, 15*time.Second) {
		t.Fatalf("left %v", files(t, dir))
	}
}

// The maker's window opens a setup.json and builds it.
func TestMakerWindow(t *testing.T) {
	fixture := testexe.Fixture(t)
	report := filepath.Join(t.TempDir(), "shots", "maker.json")
	if out, code := runExe(t, testexe.Maker(t), "-smoke", report, filepath.Join(fixture, "setup.json")); code != 0 {
		t.Fatalf("maker smoke: %d %s", code, out)
	}
	r := checkSmoke(t, report, []string{"form", "built"})
	if !r.Pages[1].Flags["built"] {
		t.Fatal("not built")
	}
	if _, err := os.Stat(filepath.Join(fixture, "dist", "ACME Wizard Fixture-1.0.0-setup.exe")); err != nil {
		t.Fatal(err)
	}
}

// An install made by a program's own -install (the other ACME programs) is taken over cleanly.
func TestTakeOverASiblingInstall(t *testing.T) {
	fixture := testexe.Fixture(t)
	setup := build(t, fixture, nil)
	root := filepath.Join(t.TempDir(), "root")
	pl := install.SandboxPlaces(root)
	dir := pl.Dir("ACME Inc", "ACME Wizard Fixture")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "hello.exe"), []byte("old exe"), 0o755)
	os.WriteFile(filepath.Join(dir, "app.ico"), []byte("old icon"), 0o644)
	os.WriteFile(filepath.Join(dir, "extra.dll"), []byte("from the old install"), 0o644)
	link := pl.StartMenuLink("ACME Inc", "ACME Wizard Fixture")
	os.MkdirAll(filepath.Dir(link), 0o755)
	os.WriteFile(link, []byte("old shortcut"), 0o644)
	marker, _ := json.Marshal(map[string]any{"app_id": "ACME.WizardFixture", "name": "ACME Wizard Fixture", "version": "0.2.0",
		"files": []string{"hello.exe", "app.ico", "extra.dll"}, "shortcuts": []string{link}})
	os.WriteFile(filepath.Join(dir, "install.json"), marker, 0o644)
	reg, _ := json.Marshal(map[string]any{key: map[string]any{"InstallLocation": dir, "DisplayVersion": "0.2.0",
		"UninstallString": `"` + filepath.Join(dir, "hello.exe") + `" -uninstall`}})
	os.WriteFile(filepath.Join(root, "registry.json"), reg, 0o644)

	out, code := runExe(t, setup, "-quiet", "-root", root)
	if code != 0 || !strings.Contains(out, "extra.dll") {
		t.Fatalf("take over: %d %s", code, out)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "hello.exe")); string(b) == "old exe" {
		t.Fatal("hello.exe not replaced")
	}
	if v := registry(t, root)[key]; !strings.Contains(v["UninstallString"].(string), "uninstall.exe") {
		t.Fatalf("registry %v", v)
	}
	if s, err := shell.ReadLink(link); err != nil || s.Target != filepath.Join(dir, "hello.exe") {
		t.Fatalf("shortcut not rewritten: %v %+v", err, s)
	}
	// and its uninstaller then removes our files but not the old install's unknown one
	if out, code := runExe(t, filepath.Join(dir, "uninstall.exe"), "-quiet", "-root", root); code != 0 {
		t.Fatalf("uninstall: %s", out)
	}
	gone(filepath.Join(dir, "uninstall.exe"), 15*time.Second)
	if got := files(t, dir); !reflect.DeepEqual(got, []string{"extra.dll"}) {
		t.Fatalf("left %v", got)
	}
}
