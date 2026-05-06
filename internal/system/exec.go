package system

import (
	"fmt"
	"os"
	"os/exec"
)

func RunCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	fmt.Println("▶ Ejecutando:", name, args)

	return cmd.Run()
}
