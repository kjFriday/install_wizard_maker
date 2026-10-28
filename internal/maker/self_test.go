// ==== Generic Company Inc. portfolio ====================================
// Project: install_wizard_maker | File: internal/maker/self_test.go
// About: ==== Generic Company Inc. portfolio ===============================================...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// ==== Generic Company Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/maker/self_test.go
// About: Tests for package maker.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

package maker

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"generic-company/wizard/internal/app"
	"generic-company/wizard/internal/install"
)

func TestRunsFrom(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Generic Company Wizard Maker")
	for _, c := range []struct {
		self string
		want bool
	}{
		{filepath.Join(dir, app.ExeName), true},
		{filepath.Join(strings.ToUpper(dir), app.ExeName), true},
		{filepath.Join(dir, "sub", app.ExeName), true},
		{filepath.Join(dir+" 2", app.ExeName), false},
		{filepath.Join(filepath.Dir(dir), app.ExeName), false},
	} {
		if got := runsFrom(c.self, dir); got != c.want {
			t.Errorf("runsFrom(%q) = %v, want %v", c.self, got, c.want)
		}
	}
}

// The installed copy refuses to uninstall itself and changes nothing; another copy uninstalls it fully.
func TestUninstallSelf(t *testing.T) {
	root := t.TempDir()
	if out, code := run(t, root, "-install", "-quiet", "-root", root); code != app.ExitOK {
		t.Fatalf("-install: %d %s", code, out)
	}
	dir := filepath.Join(root, "Programs", app.Group, app.Name)
	installed := filepath.Join(dir, app.ExeName)
	before := tree(t, root)
	for _, want := range []string{installed, filepath.Join(dir, install.MarkerName), filepath.Join(root, "registry.json")} {
		if _, ok := before[want]; !ok {
			t.Fatalf("not installed: %s missing", want)
		}
	}

	// Help > Uninstall: the installed copy must close for its uninstaller to finish; another need not
	uninstaller := filepath.Join(dir, install.UninstallerName)
	for self, wantInside := range map[string]bool{installed: true, filepath.Join(root, app.ExeName): false} {
		exe, inside, err := selfUninstaller(self, root)
		if err != nil || exe != uninstaller || inside != wantInside {
			t.Fatalf("selfUninstaller(%s) = %s %v %v", self, exe, inside, err)
		}
	}

	cmd := exec.Command(installed, "-uninstall", "-quiet", "-root", root)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	ee, _ := err.(*exec.ExitError)
	if ee == nil || ee.ExitCode() != app.ExitRefused || !strings.Contains(string(out), filepath.Join(dir, install.UninstallerName)) {
		t.Fatalf("installed copy: %v %s", err, out)
	}
	after := tree(t, root)
	if len(after) != len(before) {
		t.Fatalf("refusal changed files: %d before, %d after", len(before), len(after))
	}
	for p, data := range before {
		if after[p] != data {
			t.Fatalf("refusal changed %s", p)
		}
	}

	if out, code := run(t, root, "-uninstall", "-quiet", "-root", root); code != app.ExitOK {
		t.Fatalf("-uninstall: %d %s", code, out)
	}
	if _, err := os.Stat(installed); !os.IsNotExist(err) {
		t.Fatalf("still installed: %v", err)
	}
	if _, _, err := selfUninstaller(installed, root); err == nil {
		t.Fatal("selfUninstaller found an uninstalled maker")
	}
}

// tree maps every file under root to its contents.
func tree(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		files[p] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=f2d90b23fc5ff1eb9b308a69f9747608598c3189fd553b8c8a7c595bbc23c04a ====
