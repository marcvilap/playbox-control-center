// playbox — PlayBox VR Control Center.
//
// v0.3: descubrimiento F0, vigilancia continua (servicio Windows o ventana),
// simulación de prueba y autoactualización firmada.
// Doble clic sin argumentos abre un menú para el operador.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"playbox/internal/descubrir"
	"playbox/internal/ejec"
)

// Valores que se pueden sobrescribir al compilar con -ldflags "-X main.…=…".
var (
	Version            = "0.4.1"
	URLActualizaciones = "https://raw.githubusercontent.com/marcvilap/playbox-control-center/actualizaciones/version.json"
	ClavePublica       = "f32411d672c2759a1737dc6cf47c059db7d397ab9458134594700070f8d95312"
)

func main() {
	prepararConsola()
	args, sinActualizar := quitarFlag(os.Args[1:], "--sin-actualizar")
	if len(args) == 0 {
		os.Exit(runApp()) // doble clic: ventana con botón «Actualizar»
	}
	switch args[0] {
	case "actualizar":
		os.Exit(cmdActualizar())
	case "descubrir", "vigilar", "simular", "estado", "configurar", "menu":
		if !sinActualizar {
			autoActualizar(args)
		}
	}
	switch args[0] {
	case "menu":
		os.Exit(menu())
	case "descubrir":
		os.Exit(cmdDescubrir(args[1:]))
	case "vigilar":
		os.Exit(cmdVigilar(false))
	case "simular":
		os.Exit(cmdVigilar(true))
	case "estado":
		os.Exit(cmdEstado())
	case "configurar":
		os.Exit(cmdConfigurar())
	case "servicio":
		os.Exit(cmdServicio(args[1:]))
	case "instalar":
		ruta, err := instalarApp()
		if err != nil {
			fmt.Println("Error:", err)
			os.Exit(1)
		}
		fmt.Println("Instalado en", ruta)
	case "desinstalar-app":
		os.Exit(cmdDesinstalarApp())
	case "version", "--version", "-v":
		fmt.Println("playbox", Version)
	case "ayuda", "--help", "-h", "help":
		ayuda()
	default:
		fmt.Fprintf(os.Stderr, "Orden desconocida: %s\n\n", args[0])
		ayuda()
		os.Exit(2)
	}
}

func ayuda() {
	fmt.Printf(`PlayBox VR Control Center %s

Uso:
  playbox                      Abre la aplicación (doble clic)
  playbox menu                 Menú en consola
  playbox instalar             Instala el programa para este usuario
  playbox simular              Prueba con 10 visores virtuales (no toca nada real)
  playbox vigilar              Vigila en esta ventana (Ctrl+C para salir)
  playbox configurar           Asigna la IP de cada visor (detecta el conectado por USB)
  playbox estado               Estado que guarda el servicio
  playbox servicio instalar    Instala el servicio Windows (como administrador)
  playbox servicio desinstalar | iniciar | parar
  playbox descubrir [opciones] Inventario F0 de solo lectura
  playbox actualizar           Busca e instala una versión nueva ahora

Configuración: playbox.json junto al exe (IP de cada PICO).
Datos: carpeta datos\ (estado.json, eventos\, incidentes\).
  playbox version

Al arrancar, playbox busca versiones nuevas y se actualiza solo
(añade --sin-actualizar para saltarlo).

Opciones de descubrir:
  --fase TEXTO     reposo, partida-vex, cambio-vex-spawnpoint… (por defecto: reposo)
  --salida DIR     carpeta de informes (por defecto: informes\ junto al exe)
  --adb RUTA       ruta a adb.exe si no se encuentra solo
  --ips A,B,C      IPs extra de visores a medir con ping
  --ping-n N       paquetes por destino, 1 por segundo (por defecto: 60)
  --sin-ping  --sin-adb  --sin-logs
`, Version)
}

func dirInformes() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), "informes")
	}
	return "informes"
}

func cmdDescubrir(args []string) int {
	fs := flag.NewFlagSet("descubrir", flag.ContinueOnError)
	fase := fs.String("fase", "reposo", "")
	salida := fs.String("salida", dirInformes(), "")
	adb := fs.String("adb", "", "")
	ips := fs.String("ips", "", "")
	pingN := fs.Int("ping-n", 60, "")
	sinPing := fs.Bool("sin-ping", false, "")
	sinADB := fs.Bool("sin-adb", false, "")
	sinLogs := fs.Bool("sin-logs", false, "")
	fs.Usage = ayuda
	if err := fs.Parse(args); err != nil {
		return 2
	}
	var extra []string
	for _, ip := range strings.Split(*ips, ",") {
		if ip = strings.TrimSpace(ip); ip != "" {
			extra = append(extra, ip)
		}
	}
	_, err := ejecutar(descubrir.Opciones{
		Fase: *fase, DirBase: *salida, ADB: *adb, IPsExtra: extra, PingN: *pingN,
		SinPing: *sinPing, SinADB: *sinADB, SinLogs: *sinLogs,
	})
	if err != nil {
		return 1
	}
	return 0
}

func ejecutar(o descubrir.Opciones) (*descubrir.Resultado, error) {
	o.Version = Version
	o.Aviso = func(s string) { fmt.Println(s) }
	fmt.Printf("PlayBox Control Center %s — descubrimiento F0 (solo lectura)\nFase: %s\n\n", Version, o.Fase)
	r, err := descubrir.Ejecutar(o)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return r, err
	}
	fmt.Printf("\nListo.\n  Resumen: %s\n  Paquete: %s\n", filepath.Join(r.Carpeta, "resumen.md"), r.Zip)
	fmt.Println("Revisa el .zip antes de enviarlo: los logs pueden contener nombres de jugadores.")
	return r, nil
}

func menu() int {
	in := bufio.NewReader(os.Stdin)
	leer := func(msg, def string) string {
		fmt.Print(msg)
		s, _ := in.ReadString('\n')
		if s = strings.TrimSpace(s); s == "" {
			return def
		}
		return s
	}
	salir := func(code int) int {
		leer("\nPulsa Enter para salir…", "")
		return code
	}
	fmt.Printf("PlayBox VR Control Center %s\n\n", Version)
	fmt.Println("VIGILANCIA")
	fmt.Println("  1) Ver estado de los visores (servicio)")
	fmt.Println("  2) Simulación de prueba con visores virtuales")
	fmt.Println("  3) Vigilar en esta ventana")
	fmt.Println("  4) Instalar o actualizar el servicio (requiere administrador)")
	fmt.Println("  c) Configurar las IP de los visores")
	fmt.Println("\nDESCUBRIMIENTO F0 (centro cerrado, sin clientes)")
	fmt.Println("  5) Reposo, sin partida")
	fmt.Println("  6) Partida de prueba VEX")
	fmt.Println("  7) Partida de prueba HeroZone")
	fmt.Println("  8) Partida de prueba Spawnpoint")
	fmt.Println("  9) Justo después de cambiar de proveedor")
	fmt.Println("\n  0) Salir")
	op := leer("\nOpción: ", "")
	fmt.Println()
	switch op {
	case "0", "":
		return 0
	case "1":
		return salir(cmdEstado())
	case "2":
		return salir(cmdVigilar(true))
	case "3":
		return salir(cmdVigilar(false))
	case "4":
		return salir(cmdServicio([]string{"instalar"}))
	case "c", "C":
		return salir(cmdConfigurar())
	}
	fase := map[string]string{"5": "reposo", "6": "partida-vex", "7": "partida-herozone", "8": "partida-spawnpoint"}
	f, ok := fase[op]
	if op == "9" {
		f, ok = "cambio-"+leer("¿De cuál a cuál? (ej. vex-spawnpoint): ", "proveedor"), true
	}
	if !ok {
		fmt.Println("Opción no válida.")
		return salir(2)
	}
	r, err := ejecutar(descubrir.Opciones{Fase: f, DirBase: dirInformes(), PingN: 60})
	if err == nil && ejec.EsWindows() {
		_ = exec.Command("explorer.exe", r.Carpeta).Start()
	}
	if err != nil {
		return salir(1)
	}
	return salir(0)
}
