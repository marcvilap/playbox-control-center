//go:build !windows

package main

import "fmt"

func cmdServicio(args []string) int {
	if len(args) > 0 && args[0] == "ejecutar" {
		return cmdVigilar(false)
	}
	fmt.Println("El servicio solo existe en Windows; usa «playbox vigilar» en este sistema.")
	return 2
}
