package adb

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"playbox/internal/ejec"
)

// PuertoWifi es el puerto en el que la gafa escucha ADB por Wi-Fi tras «tcpip».
const PuertoWifi = "5555"

// Telemetria es lo que se lee de una gafa por ADB (estado OBSERVADO).
type Telemetria struct {
	BateriaPct *int      `json:"bateria_pct"`
	Cargando   bool      `json:"cargando"`
	TempC      *float64  `json:"temp_c"`
	Pantalla   string    `json:"pantalla"` // Activa, Reposo
	App        string    `json:"app"`      // paquete en primer plano
	EnMenu     bool      `json:"en_menu"`  // INFERIDO: el paquete parece el launcher de PICO
	Leido      time.Time `json:"leido"`
	Error      string    `json:"error,omitempty"`
}

// ActivarWifi deja la gafa escuchando ADB por Wi-Fi (hasta que se reinicie).
func ActivarWifi(adb, serie string) error {
	code, out := ejec.Run(20*time.Second, adb, "-s", serie, "tcpip", PuertoWifi)
	if code != 0 || strings.Contains(strings.ToLower(out), "error") {
		return fmt.Errorf("la gafa no aceptó el modo Wi-Fi: %s", ejec.Stdout(out))
	}
	return nil
}

// Conectar enlaza con la gafa por Wi-Fi (adb connect ip:5555).
func Conectar(adb, ip string) error {
	_, out := ejec.Run(10*time.Second, adb, "connect", ip+":"+PuertoWifi)
	o := strings.ToLower(out)
	if strings.Contains(o, "connected to") {
		return nil
	}
	return fmt.Errorf("no conecta por Wi-Fi (%s)", strings.TrimSpace(ejec.Stdout(out)))
}

// Una sola llamada a la gafa, con los filtros ejecutados dentro de ella para
// transferir solo unas pocas líneas.
const consulta = `dumpsys battery; echo @@@; dumpsys power | grep -m1 mWakefulness=; echo @@@; dumpsys activity activities | grep -m1 -E 'mResumedActivity|ResumedActivity'`

// LeerTelemetria pide batería, pantalla y app en primer plano a la gafa.
func LeerTelemetria(adb, ip string) Telemetria {
	destino := ip + ":" + PuertoWifi
	code, out := ejec.Run(15*time.Second, adb, "-s", destino, "shell", consulta)
	if code != 0 || strings.Contains(out, "not found") || strings.Contains(out, "offline") {
		if Conectar(adb, ip) == nil {
			code, out = ejec.Run(15*time.Second, adb, "-s", destino, "shell", consulta)
		}
	}
	if code != 0 {
		return Telemetria{Leido: time.Now(), Error: "sin conexión ADB por Wi-Fi (¿se reinició la gafa?)"}
	}
	return ParsearTelemetria(ejec.Stdout(out))
}

var (
	reNivel     = regexp.MustCompile(`(?m)^\s*level:\s*(\d+)`)
	reTemp      = regexp.MustCompile(`(?m)^\s*temperature:\s*(\d+)`)
	reCarga     = regexp.MustCompile(`(?m)^\s*(?:AC|USB|Wireless|Dock) powered:\s*true`)
	reStatus    = regexp.MustCompile(`(?m)^\s*status:\s*(\d+)`)
	reDespierta = regexp.MustCompile(`mWakefulness=(\w+)`)
	reApp       = regexp.MustCompile(`ResumedActivity.*?\s([\w.]+)/`)
)

// ParsearTelemetria interpreta la salida combinada de la consulta.
func ParsearTelemetria(txt string) Telemetria {
	t := Telemetria{Leido: time.Now()}
	partes := strings.Split(txt, "@@@")
	for len(partes) < 3 {
		partes = append(partes, "")
	}
	bat, pow, act := partes[0], partes[1], partes[2]
	if n, err := strconv.Atoi(firstGroup(reNivel, bat)); err == nil {
		t.BateriaPct = &n
	}
	if n, err := strconv.Atoi(firstGroup(reTemp, bat)); err == nil {
		c := float64(n) / 10
		t.TempC = &c
	}
	t.Cargando = reCarga.MatchString(bat) || firstGroup(reStatus, bat) == "2"
	switch firstGroup(reDespierta, pow) {
	case "Awake":
		t.Pantalla = "Activa"
	case "Asleep", "Dozing", "Dreaming":
		t.Pantalla = "Reposo"
	}
	t.App = firstGroup(reApp, act)
	a := strings.ToLower(t.App)
	t.EnMenu = strings.Contains(a, "launcher") || strings.HasSuffix(a, ".home") || strings.Contains(a, "vrshell")
	return t
}

func firstGroup(re *regexp.Regexp, s string) string {
	if m := re.FindStringSubmatch(s); len(m) > 1 {
		return m[1]
	}
	return ""
}
