package main

import (
	"fmt"
	"os"
	"os/exec"

	"playbox/internal/actualizar"
)

const envRecienActualizado = "PLAYBOX_RECIEN_ACTUALIZADO"

func configActualizacion() actualizar.Config {
	u := URLActualizaciones
	if e := os.Getenv("PLAYBOX_ACTUALIZACIONES"); e != "" {
		u = e // permite apuntar a otro servidor sin recompilar
	}
	return actualizar.Config{URLManifiesto: u, ClavePublica: ClavePublica, VersionActual: Version}
}

// autoActualizar comprueba al arrancar; si hay versión nueva la instala y
// relanza el programa con los mismos argumentos. Sin conexión, sigue.
func autoActualizar(args []string) {
	actualizar.Limpiar()
	if os.Getenv(envRecienActualizado) != "" {
		fmt.Printf("Actualizado a la versión %s.\n\n", Version)
		return
	}
	c := configActualizacion()
	m, hayNueva, err := actualizar.Comprobar(c)
	if err != nil {
		fmt.Printf("(No se pudo comprobar si hay actualizaciones; sigo con la %s)\n\n", Version)
		return
	}
	if !hayNueva {
		return
	}
	fmt.Printf("Hay una versión nueva: %s (tienes la %s). Actualizando…\n", m.Version, Version)
	if m.Notas != "" {
		fmt.Println("Novedades:", m.Notas)
	}
	ruta, err := actualizar.Instalar(c, m)
	if err != nil {
		fmt.Printf("No se pudo actualizar: %v\nSigo con la versión %s.\n\n", err, Version)
		return
	}
	cmd := exec.Command(ruta, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(), envRecienActualizado+"=1")
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		fmt.Println("Versión nueva instalada; vuelve a abrir playbox.")
		os.Exit(1)
	}
	os.Exit(0)
}

func cmdActualizar() int {
	actualizar.Limpiar()
	c := configActualizacion()
	fmt.Printf("Versión instalada: %s\nComprobando %s…\n", Version, c.URLManifiesto)
	m, hayNueva, err := actualizar.Comprobar(c)
	if err != nil {
		fmt.Println("Error:", err)
		return 1
	}
	if !hayNueva {
		fmt.Printf("Ya tienes la última versión (%s).\n", Version)
		return 0
	}
	fmt.Printf("Instalando %s…\n", m.Version)
	if _, err := actualizar.Instalar(c, m); err != nil {
		fmt.Println("Error:", err)
		return 1
	}
	fmt.Printf("Listo: la próxima vez se abrirá la %s.\n", m.Version)
	return 0
}

func quitarFlag(args []string, flag string) ([]string, bool) {
	var out []string
	encontrado := false
	for _, a := range args {
		if a == flag {
			encontrado = true
			continue
		}
		out = append(out, a)
	}
	return out, encontrado
}

func esVersionOAyuda(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "version", "--version", "-v", "ayuda", "--help", "-h", "help":
		return true
	}
	return false
}
