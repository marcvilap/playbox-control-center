// Package adb habla con las gafas conectadas por USB mediante la herramienta
// oficial de Google (platform-tools). Solo lee datos (getprop, ip, status).
package adb

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"playbox/internal/ejec"
)

// URLPlatformTools es la descarga oficial de Google para Windows.
const URLPlatformTools = "https://dl.google.com/android/repository/platform-tools-latest-windows.zip"

// Gafa es lo que se lee de unas gafas conectadas por USB.
type Gafa struct {
	Serie    string `json:"serie"`
	Estado   string `json:"estado"` // device, unauthorized, offline…
	Modelo   string `json:"modelo"`
	Firmware string `json:"firmware"`
	MAC      string `json:"mac"`
	IP       string `json:"ip"`
	SSID     string `json:"ssid"`
}

// Localizar busca adb.exe en la carpeta indicada, el PATH o rutas típicas.
func Localizar(dirPropio string) string {
	existe := func(p string) bool { _, err := os.Stat(p); return err == nil }
	nombre := "adb"
	if ejec.EsWindows() {
		nombre = "adb.exe"
	}
	cands := []string{filepath.Join(dirPropio, "platform-tools", nombre)}
	if p, err := exec.LookPath("adb"); err == nil {
		cands = append(cands, p)
	}
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		cands = append(cands, filepath.Join(la, "Android", "Sdk", "platform-tools", nombre))
	}
	cands = append(cands, `C:\platform-tools\adb.exe`)
	for _, c := range cands {
		if existe(c) {
			return c
		}
	}
	return ""
}

// Descargar baja platform-tools de Google y extrae solo adb y sus DLL en
// dirPropio/platform-tools. Devuelve la ruta de adb.
func Descargar(ctx context.Context, dirPropio string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, URLPlatformTools, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("no se pudo descargar de Google: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Google respondió %s", resp.Status)
	}
	tmp, err := os.CreateTemp("", "platform-tools-*.zip")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, io.LimitReader(resp.Body, 200<<20)); err != nil {
		tmp.Close()
		return "", err
	}
	tmp.Close()
	z, err := zip.OpenReader(tmp.Name())
	if err != nil {
		return "", fmt.Errorf("descarga dañada: %w", err)
	}
	defer z.Close()
	destino := filepath.Join(dirPropio, "platform-tools")
	if err := os.MkdirAll(destino, 0o755); err != nil {
		return "", err
	}
	quiero := map[string]bool{"adb.exe": true, "AdbWinApi.dll": true, "AdbWinUsbApi.dll": true, "adb": true}
	extraidos := 0
	for _, f := range z.File {
		base := filepath.Base(f.Name)
		if !quiero[base] || f.FileInfo().IsDir() {
			continue
		}
		src, err := f.Open()
		if err != nil {
			return "", err
		}
		dst, err := os.OpenFile(filepath.Join(destino, base), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			src.Close()
			return "", err
		}
		_, err = io.Copy(dst, src)
		src.Close()
		dst.Close()
		if err != nil {
			return "", err
		}
		extraidos++
	}
	if extraidos == 0 {
		return "", fmt.Errorf("el paquete de Google no contiene adb")
	}
	return Localizar(dirPropio), nil
}

// Dispositivos lista las gafas conectadas por USB (serie y estado).
func Dispositivos(adb string) ([]Gafa, error) {
	code, out := ejec.Run(15*time.Second, adb, "devices")
	if code != 0 {
		return nil, fmt.Errorf("adb no responde: %s", ejec.Stdout(out))
	}
	var gs []Gafa
	for i, l := range strings.Split(ejec.Stdout(out), "\n") {
		f := strings.Fields(l)
		if i == 0 || len(f) < 2 || strings.HasPrefix(l, "*") {
			continue
		}
		if strings.Contains(f[0], ":") {
			continue // conexión por Wi-Fi (ip:5555) de una gafa ya vista por USB
		}
		gs = append(gs, Gafa{Serie: f[0], Estado: f[1]})
	}
	return gs, nil
}

var (
	reMAC  = regexp.MustCompile(`link/ether\s+([0-9a-fA-F:]{17})`)
	reIP   = regexp.MustCompile(`inet\s+(\d+\.\d+\.\d+\.\d+)`)
	reSSID = regexp.MustCompile(`SSID:\s*"?([^",]+)`)
)

// Interpretar extrae MAC/IP/SSID de las salidas de «ip addr show wlan0» y
// «cmd wifi status».
func Interpretar(g *Gafa, ipAddr, wifi string) {
	if m := reMAC.FindStringSubmatch(ipAddr); m != nil {
		g.MAC = strings.ToLower(m[1])
	}
	if m := reIP.FindStringSubmatch(ipAddr); m != nil {
		g.IP = m[1]
	}
	if m := reSSID.FindStringSubmatch(wifi); m != nil {
		g.SSID = strings.TrimSpace(m[1])
	}
}

// Leer completa los datos de unas gafas autorizadas (solo lectura).
func Leer(adb string, g *Gafa) {
	sh := func(args ...string) string {
		_, out := ejec.Run(10*time.Second, adb, append([]string{"-s", g.Serie, "shell"}, args...)...)
		return ejec.Stdout(out)
	}
	g.Modelo = sh("getprop", "ro.product.model")
	g.Firmware = sh("getprop", "ro.build.display.id")
	Interpretar(g, sh("ip", "addr", "show", "wlan0"), sh("cmd", "wifi", "status"))
}
