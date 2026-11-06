//go:build windows

package ejec

import (
	"os/exec"
	"syscall"
)

// ocultarVentana evita que cada comando abra una consola parpadeante.
func ocultarVentana(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
