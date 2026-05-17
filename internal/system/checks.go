package system

import (
	"fmt"
	"os/exec"
)

// CheckCommand returns an error if name is not found in PATH.
// Prefer exec.LookPath directly for display purposes (e.g. doctor command).
func CheckCommand(name string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("%s: not found in PATH", name)
	}
	return nil
}
