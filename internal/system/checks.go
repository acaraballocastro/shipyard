package system

import (
	"fmt"
	"os/exec"
)

func CheckCommand(name string) error {
	_, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("%s no está instalado", name)
	}
	return nil
}