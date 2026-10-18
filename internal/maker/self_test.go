package maker

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"acme/wizard/internal/app"
	"acme/wizard/internal/install"
)

func TestRunsFrom(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ACME Wizard Maker")
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
