//go:build !windows

package main

import "errors"

func estaInstalado() bool { return true }

func instalarApp() (string, error) {
	return "", errors.New("la instalación solo existe en Windows")
}

func cmdDesinstalarApp() int { return 0 }
