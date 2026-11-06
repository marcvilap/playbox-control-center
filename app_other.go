//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/signal"
)

func liberarInstanciaUnica() {}

// runApp en sistemas que no son Windows: sirve la interfaz y muestra la URL
// (para desarrollo y pruebas).
func runApp() int {
	a, url, err := iniciarApp()
	if err != nil {
		fmt.Println("Error:", err)
		return 1
	}
	fmt.Println("Interfaz en:", url)
	fin := make(chan os.Signal, 1)
	signal.Notify(fin, os.Interrupt)
	cerrar := make(chan struct{})
	a.salir = func() { close(cerrar) }
	select {
	case <-fin:
	case <-cerrar:
	}
	a.pararMonitor()
	return 0
}
