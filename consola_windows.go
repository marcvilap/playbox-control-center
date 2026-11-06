//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// prepararConsola: el exe se compila como aplicación de ventana, así que
// al usarlo por línea de órdenes se engancha a la consola desde la que se
// lanzó. Activa UTF-8 (tildes) y los códigos ANSI de la vista «vigilar».
func prepararConsola() {
	if len(os.Args) < 2 {
		return // doble clic: ventana, sin consola
	}
	// Salida ya redirigida (a un archivo o tubería): se respeta tal cual.
	if h, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE); err == nil && h != 0 && h != windows.InvalidHandle {
		if t, _ := windows.GetFileType(h); t == windows.FILE_TYPE_DISK || t == windows.FILE_TYPE_PIPE {
			return
		}
	}
	k := windows.NewLazySystemDLL("kernel32.dll")
	if r, _, _ := k.NewProc("AttachConsole").Call(uintptr(^uint32(0))); r == 0 {
		// Sin consola padre: el servicio y la desinstalación no la necesitan;
		// cualquier otra orden abre una consola propia.
		if esServicioOSilencioso() {
			return
		}
		if r, _, _ := k.NewProc("AllocConsole").Call(); r == 0 {
			return
		}
	}
	if f, err := os.OpenFile("CONOUT$", os.O_RDWR, 0); err == nil {
		os.Stdout, os.Stderr = f, f
	}
	if f, err := os.OpenFile("CONIN$", os.O_RDWR, 0); err == nil {
		os.Stdin = f
	}
	_ = windows.SetConsoleOutputCP(65001)
	_ = windows.SetConsoleCP(65001)
	if h, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE); err == nil {
		var modo uint32
		if windows.GetConsoleMode(h, &modo) == nil {
			_ = windows.SetConsoleMode(h, modo|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
		}
	}
}

// esServicioOSilencioso: órdenes que nunca deben abrir una consola nueva.
func esServicioOSilencioso() bool {
	return len(os.Args) > 1 && (os.Args[1] == "servicio" || os.Args[1] == "desinstalar-app")
}
