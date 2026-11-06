package descubrir

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"playbox/internal/ejec"
)

type adbCmd struct {
	nombre string
	args   []string
}

// Todos los comandos son de lectura (getprop, dumpsys, list, status).
var adbCmds = []adbCmd{
	{"owners", []string{"shell", "dpm", "list-owners"}},
	{"device_policy", []string{"shell", "dumpsys", "device_policy"}},
	{"serial", []string{"shell", "getprop", "ro.serialno"}},
	{"modelo", []string{"shell", "getprop", "ro.product.model"}},
	{"firmware", []string{"shell", "getprop", "ro.build.display.id"}},
	{"android", []string{"shell", "getprop", "ro.build.version.release"}},
	{"paquetes", []string{"shell", "pm", "list", "packages", "-3", "--show-versioncode"}},
	{"bateria", []string{"shell", "dumpsys", "battery"}},
	{"wifi", []string{"shell", "cmd", "wifi", "status"}},
	{"ip", []string{"shell", "ip", "-f", "inet", "addr", "show", "wlan0"}},
	{"actividad", []string{"shell", "dumpsys", "activity", "activities"}},
	{"termica", []string{"shell", "dumpsys", "thermalservice"}},
	{"doze", []string{"shell", "dumpsys", "deviceidle"}},
	{"energia", []string{"shell", "dumpsys", "power"}},
}

// Visor es el resumen de un PICO leído por ADB.
type Visor struct {
	Serial         string   `json:"serial"`
	Modelo         string   `json:"modelo"`
	Firmware       string   `json:"firmware"`
	Android        string   `json:"android"`
	DeviceOwner    string   `json:"device_owner"`
	BateriaPct     *int     `json:"bateria_pct"`
	TempBateriaC   *float64 `json:"temp_bateria_c"`
	Cargando       bool     `json:"cargando"`
	RSSIdBm        string   `json:"rssi_dbm"`
	FrecuenciaMHz  string   `json:"frecuencia_mhz"`
	SSID           string   `json:"ssid"`
	IP             string   `json:"ip"`
	AppPrimerPlano string   `json:"app_primer_plano"`
	AppsTerceros   int      `json:"apps_terceros"`
}

// ResumenVisores agrupa los visores autorizados y los que no respondieron.
type ResumenVisores struct {
	Error         string   `json:"error,omitempty"`
	Visores       []Visor  `json:"visores"`
	NoAutorizados []string `json:"no_autorizados"`
}

var (
	reOwnerComp  = regexp.MustCompile(`ComponentInfo\{([\w.]+)/`)
	reOwnerPaq   = regexp.MustCompile(`([a-z]\w*(?:\.\w+)+)/[\w.$]+`)
	reNivel      = regexp.MustCompile(`(?m)^\s*level:\s*(\d+)`)
	reTemp       = regexp.MustCompile(`(?m)^\s*temperature:\s*(\d+)`)
	reCargando   = regexp.MustCompile(`(?m)^\s*(?:AC|USB|Wireless|Dock) powered:\s*true`)
	reRSSI       = regexp.MustCompile(`RSSI:\s*(-?\d+)`)
	reFrecuencia = regexp.MustCompile(`Frequency:\s*(\d+)`)
	reSSID       = regexp.MustCompile(`SSID:\s*"?([^",]+)`)
	reIP         = regexp.MustCompile(`inet\s+(\d+\.\d+\.\d+\.\d+)`)
	reFoco       = regexp.MustCompile(`ResumedActivity.*?\s([\w.]+)/`)
)

// ResumirVisor extrae los campos clave de las salidas de ADB.
func ResumirVisor(d map[string]string) Visor {
	v := Visor{
		Serial:         strings.TrimSpace(d["serial"]),
		Modelo:         strings.TrimSpace(d["modelo"]),
		Firmware:       strings.TrimSpace(d["firmware"]),
		Android:        strings.TrimSpace(d["android"]),
		Cargando:       reCargando.MatchString(d["bateria"]),
		RSSIdBm:        Buscar(reRSSI, d["wifi"]),
		FrecuenciaMHz:  Buscar(reFrecuencia, d["wifi"]),
		SSID:           Buscar(reSSID, d["wifi"]),
		IP:             Buscar(reIP, d["ip"]),
		AppPrimerPlano: Buscar(reFoco, d["actividad"]),
	}
	owners := d["owners"]
	v.DeviceOwner = Buscar(reOwnerComp, owners)
	if v.DeviceOwner == "" {
		v.DeviceOwner = Buscar(reOwnerPaq, owners)
	}
	if v.DeviceOwner == "" {
		v.DeviceOwner = strings.Join(strings.Fields(owners), " ")
		if len(v.DeviceOwner) > 120 {
			v.DeviceOwner = v.DeviceOwner[:120]
		}
	}
	if n, err := strconv.Atoi(Buscar(reNivel, d["bateria"])); err == nil {
		v.BateriaPct = &n
	}
	if t, err := strconv.Atoi(Buscar(reTemp, d["bateria"])); err == nil {
		c := float64(t) / 10
		v.TempBateriaC = &c
	}
	for _, l := range strings.Split(d["paquetes"], "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "package:") {
			v.AppsTerceros++
		}
	}
	return v
}

// LocalizarADB busca adb.exe en la ruta indicada, el PATH, junto al exe o en rutas típicas.
func LocalizarADB(ruta string) string {
	existe := func(p string) bool { _, err := os.Stat(p); return err == nil }
	if ruta != "" {
		if existe(ruta) {
			return ruta
		}
		return ""
	}
	if p, err := exec.LookPath("adb"); err == nil {
		return p
	}
	var cands []string
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		cands = append(cands, filepath.Join(dir, "platform-tools", "adb.exe"), filepath.Join(dir, "adb.exe"))
	}
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		cands = append(cands, filepath.Join(la, "Android", "Sdk", "platform-tools", "adb.exe"))
	}
	cands = append(cands, `C:\platform-tools\adb.exe`)
	for _, c := range cands {
		if existe(c) {
			return c
		}
	}
	return ""
}

func recogerVisores(out, adb string) ResumenVisores {
	if adb == "" {
		return ResumenVisores{Error: "adb no encontrado: deja la carpeta platform-tools junto a playbox.exe o usa --adb RUTA"}
	}
	dir := filepath.Join(out, "3_visores")
	_, lista := ejec.Run(30*time.Second, adb, "devices")
	guardar(dir, "adb_devices", lista)
	var r ResumenVisores
	var series []string
	for i, l := range strings.Split(ejec.Stdout(lista), "\n") {
		f := strings.Fields(l)
		if i == 0 || len(f) < 2 {
			continue
		}
		if f[1] == "device" {
			series = append(series, f[0])
		} else {
			r.NoAutorizados = append(r.NoAutorizados, f[0]+" ("+f[1]+")")
		}
	}
	for _, serie := range series {
		datos := map[string]string{}
		sub := filepath.Join(dir, nombreSeguro(serie))
		for _, c := range adbCmds {
			_, txt := ejec.Run(60*time.Second, adb, append([]string{"-s", serie}, c.args...)...)
			datos[c.nombre] = txt
			guardar(sub, c.nombre, txt)
		}
		r.Visores = append(r.Visores, ResumirVisor(datos))
	}
	return r
}
