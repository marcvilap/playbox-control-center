//go:build !windows

package ejec

import "os/exec"

func ocultarVentana(cmd *exec.Cmd) {}
