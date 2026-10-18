package wizard

import (
	"os"

	"acme/wizard/internal/python"
)

// findPython is the pythonw.exe of the newest registered CPython of at least min, or "".
var findPython = func(min string) string {
	found, ok := python.Find(python.Windows{}, min, func(p string) bool {
		info, err := os.Stat(p)
		return err == nil && !info.IsDir()
	})
	if !ok {
		return ""
	}
	return found.Pythonw
}
