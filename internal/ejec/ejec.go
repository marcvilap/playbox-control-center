// Package ejec ejecuta comandos externos de solo lectura con límite de tiempo.
package ejec

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// SepStderr separa la salida estándar de la de error en el texto devuelto.
const SepStderr = "\n[stderr]\n"

const psPrefijo = "[Console]::OutputEncoding=[Text.Encoding]::UTF8; $ErrorActionPreference='SilentlyContinue'; "

// EsWindows indica si el programa corre en Windows.
func EsWindows() bool { return runtime.GOOS == "windows" }

// Run ejecuta un comando y devuelve código de salida y texto (stdout + stderr).
func Run(timeout time.Duration, nombre string, args ...string) (int, string) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, nombre, args...)
	ocultarVentana(cmd)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	s := out.String()
	if strings.TrimSpace(errb.String()) != "" {
		s += SepStderr + errb.String()
	}
	s = strings.ToValidUTF8(s, "?")
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return 124, s + "\n[tiempo agotado tras " + timeout.String() + "]"
	}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode(), s
		}
		return 127, "[no se pudo ejecutar] " + nombre + ": " + err.Error()
	}
	return 0, s
}

// PS ejecuta un script de PowerShell con salida UTF-8.
func PS(timeout time.Duration, script string) (int, string) {
	return Run(timeout, "powershell.exe", "-NoProfile", "-NonInteractive",
		"-ExecutionPolicy", "Bypass", "-Command", psPrefijo+script)
}

// Stdout devuelve solo la parte de salida estándar del texto de Run.
func Stdout(s string) string {
	return strings.TrimSpace(strings.SplitN(s, SepStderr, 2)[0])
}
