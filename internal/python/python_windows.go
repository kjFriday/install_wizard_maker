package python

import "acme/wizard/internal/reg"

// Windows is the real registry.
type Windows struct{}

// Subkeys lists a key's subkeys.
func (Windows) Subkeys(root uintptr, path string) ([]string, error) { return reg.Subkeys(root, path) }

// String reads a string value.
func (Windows) String(root uintptr, path, name string) (string, bool) {
	return reg.String(root, path, name)
}
