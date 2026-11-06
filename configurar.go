package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"playbox/internal/descubrir"
	"playbox/internal/ejec"
	"playbox/internal/monitor"
)

// cmdConfigurar asigna IPs a los visores sin editar el JSON a mano.
// Si hay un visor conectado por USB, propone su IP automáticamente.
func cmdConfigurar() int {
	cfg, _, err := monitor.Cargar(rutaConfig())
	if err != nil {
		fmt.Println("Error:", err)
		return 1
	}
	in := bufio.NewReader(os.Stdin)
	leer := func(msg string) string {
		fmt.Print(msg)
		s, _ := in.ReadString('\n')
		return strings.TrimSpace(s)
	}
	mostrar := func() {
		fmt.Println("\nVisores configurados:")
		for i, v := range cfg.Visores {
			ip := v.IP
			if ip == "" {
				ip = "(sin IP)"
			}
			fmt.Printf("  %2d) %s  %s\n", i+1, v.ID, ip)
		}
	}

	if ip, serie := ipVisorUSB(); ip != "" {
		fmt.Printf("He encontrado un visor por USB (serie %s) con IP %s.\n", serie, ip)
		if n := leer("¿A qué número lo asigno? (1-10, Enter = no asignar): "); n != "" {
			if i, err := strconv.Atoi(n); err == nil && i >= 1 && i <= len(cfg.Visores) {
				cfg.Visores[i-1].IP = ip
			}
		}
	}
	for {
		mostrar()
		n := leer("\nNúmero de visor a cambiar (Enter = guardar y salir): ")
		if n == "" {
			break
		}
		i, err := strconv.Atoi(n)
		if err != nil || i < 1 || i > len(cfg.Visores) {
			fmt.Println("Número no válido.")
			continue
		}
		ip := leer(fmt.Sprintf("IP de %s (vacío = quitar): ", cfg.Visores[i-1].ID))
		if ip != "" && net.ParseIP(ip).To4() == nil {
			fmt.Println("Eso no es una IP válida (ejemplo: 192.168.0.101).")
			continue
		}
		cfg.Visores[i-1].IP = ip
	}
	if err := monitor.Guardar(rutaConfig(), cfg); err != nil {
		fmt.Println("No se pudo guardar:", err)
		return 1
	}
	fmt.Printf("Guardado en %s (%d visores con IP).\n", rutaConfig(), cfg.VisoresConIP())
	fmt.Println("Si el servicio está instalado, reinícialo para aplicar los cambios: playbox servicio parar / iniciar")
	return 0
}

// ipVisorUSB lee por ADB (solo lectura) la IP Wi-Fi del primer visor conectado.
func ipVisorUSB() (ip, serie string) {
	adb := descubrir.LocalizarADB("")
	if adb == "" {
		return "", ""
	}
	_, lista := ejec.Run(10*time.Second, adb, "devices")
	for i, l := range strings.Split(ejec.Stdout(lista), "\n") {
		f := strings.Fields(l)
		if i == 0 || len(f) < 2 || f[1] != "device" {
			continue
		}
		_, out := ejec.Run(10*time.Second, adb, "-s", f[0], "shell", "ip", "-f", "inet", "addr", "show", "wlan0")
		v := descubrir.ResumirVisor(map[string]string{"ip": out})
		return v.IP, f[0]
	}
	return "", ""
}
