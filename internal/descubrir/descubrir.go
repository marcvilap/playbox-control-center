// Package descubrir implementa la fase F0: inventario de solo lectura del
// PC, el software de proveedores, los visores PICO y la red.
package descubrir

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"playbox/internal/ejec"
)

// Opciones controla una ejecución del descubrimiento.
type Opciones struct {
	Fase     string
	DirBase  string
	ADB      string
	IPsExtra []string
	PingN    int
	SinPing  bool
	SinADB   bool
	SinLogs  bool
	Version  string
	Aviso    func(string)
}

// Resultado reúne el resumen y dónde quedó guardado.
type Resultado struct {
	Meta        map[string]string  `json:"meta"`
	Windows     ResumenWindows     `json:"windows"`
	Proveedores ResumenProveedores `json:"proveedores"`
	Visores     ResumenVisores     `json:"visores"`
	Red         ResumenRed         `json:"red"`
	Carpeta     string             `json:"-"`
	Zip         string             `json:"-"`
}

var reFase = regexp.MustCompile(`[^\w-]`)

// Ejecutar recoge todo, escribe resumen.md / resumen.json y empaqueta un .zip.
func Ejecutar(o Opciones) (*Resultado, error) {
	if o.Aviso == nil {
		o.Aviso = func(string) {}
	}
	if o.PingN <= 0 {
		o.PingN = 60
	}
	ahora := time.Now()
	nombre := fmt.Sprintf("f0_%s_%s", ahora.Format("20060102_1504"), reFase.ReplaceAllString(o.Fase, "_"))
	out := filepath.Join(o.DirBase, nombre)
	if err := os.MkdirAll(out, 0o755); err != nil {
		return nil, err
	}
	equipo, _ := os.Hostname()
	r := &Resultado{Carpeta: out, Meta: map[string]string{
		"fase": o.Fase, "fecha": ahora.Format("2006-01-02T15:04:05"), "equipo": equipo,
		"version": o.Version, "so": runtime.GOOS,
	}}

	if ejec.EsWindows() {
		o.Aviso("1/4 PC Windows…")
		r.Windows = recogerWindows(out)
		o.Aviso("2/4 Software de proveedores…")
		r.Proveedores = recogerProveedores(out, !o.SinLogs)
	} else {
		o.Aviso("1-2/4 Omitido: no es Windows")
	}
	o.Aviso("3/4 Visores (ADB)…")
	if o.SinADB {
		r.Visores = ResumenVisores{Error: "omitido (--sin-adb)"}
	} else {
		r.Visores = recogerVisores(out, LocalizarADB(o.ADB))
	}
	o.Aviso("4/4 Red…")
	var ips []string
	for _, v := range r.Visores.Visores {
		if v.IP != "" {
			ips = append(ips, v.IP)
		}
	}
	r.Red = recogerRed(out, append(ips, o.IPsExtra...), o.PingN, !o.SinPing, o.Aviso)

	j, _ := json.MarshalIndent(r, "", "  ")
	if err := os.WriteFile(filepath.Join(out, "resumen.json"), j, 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(out, "resumen.md"), []byte(Informe(r)), 0o644); err != nil {
		return nil, err
	}
	r.Zip = out + ".zip"
	if err := comprimir(out, r.Zip); err != nil {
		return r, err
	}
	return r, nil
}

func comprimir(dir, destino string) error {
	f, err := os.Create(destino)
	if err != nil {
		return err
	}
	defer f.Close()
	z := zip.NewWriter(f)
	base := filepath.Dir(dir)
	err = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(base, p)
		w, err := z.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		src, err := os.Open(p)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(w, src)
		return err
	})
	if cerr := z.Close(); err == nil {
		err = cerr
	}
	return err
}

func lista(xs []string, vacio string) []string {
	if len(xs) == 0 {
		return []string{"- " + vacio}
	}
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = "- " + x
	}
	return out
}

func o(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// Informe genera el resumen legible en Markdown.
func Informe(r *Resultado) string {
	m := r.Meta
	L := []string{
		fmt.Sprintf("# PlayBox F0 — %s — %s", m["fase"], m["fecha"]), "",
		fmt.Sprintf("Equipo: %s · playbox %s · Solo lectura", m["equipo"], m["version"]), "",
		"## 1. PC Windows",
		"- SO: " + o(r.Windows.SO),
		"- Últimas actualizaciones: " + o(strings.Join(r.Windows.Actualizaciones, ", ")),
		"- Más errores (30 días): " + o(strings.Join(r.Windows.Errores30d, "; ")),
		"- USB/HID con problemas: " + o(strings.Join(r.Windows.HIDProblemas, ", ")), "",
		"## 2. Proveedores en el PC", "", "**Servicios**",
	}
	L = append(L, lista(r.Proveedores.Servicios, "ninguno detectado")...)
	L = append(L, "", "**Procesos**")
	L = append(L, lista(r.Proveedores.Procesos, "ninguno detectado")...)
	L = append(L, "", "**Puertos**")
	L = append(L, lista(r.Proveedores.Puertos, "ninguno detectado")...)
	L = append(L, "", "**Carpetas**")
	L = append(L, lista(r.Proveedores.Carpetas, "ninguna detectada")...)
	L = append(L, fmt.Sprintf("- Colas de logs copiadas: %d", r.Proveedores.LogsCopiados), "", "## 3. Visores")
	v := r.Visores
	if v.Error != "" {
		L = append(L, "- "+v.Error)
	}
	if len(v.NoAutorizados) > 0 {
		L = append(L, "- Sin autorizar u offline: "+strings.Join(v.NoAutorizados, ", "))
	}
	if len(v.Visores) > 0 {
		L = append(L, "", "| Serie | Firmware | Device Owner | Bat % | Temp °C | RSSI dBm | MHz | IP | Primer plano |",
			"|---|---|---|---|---|---|---|---|---|")
		for _, x := range v.Visores {
			bat, tmp := "—", "—"
			if x.BateriaPct != nil {
				bat = fmt.Sprint(*x.BateriaPct)
			}
			if x.TempBateriaC != nil {
				tmp = fmt.Sprintf("%.1f", *x.TempBateriaC)
			}
			L = append(L, fmt.Sprintf("| %s | %s | %s | %s | %s | %s | %s | %s | %s |",
				o(x.Serial), o(x.Firmware), o(x.DeviceOwner), bat, tmp, o(x.RSSIdBm), o(x.FrecuenciaMHz), o(x.IP), o(x.AppPrimerPlano)))
		}
	}
	L = append(L, "", "## 4. Red", "- Puerta de enlace: "+o(r.Red.Gateway))
	if len(r.Red.Pings) > 0 {
		L = append(L, "", "| Destino | Pérdida % | Mín ms | Media ms | Máx ms | Jitter ms |", "|---|---|---|---|---|---|")
		for _, p := range r.Red.Pings {
			j := "—"
			if p.JitterMs != nil {
				j = fmt.Sprintf("%.1f", *p.JitterMs)
			}
			L = append(L, fmt.Sprintf("| %s | %s | %s | %s | %s | %s |", p.IP, o(p.PerdidaPct), o(p.MinMs), o(p.MediaMs), o(p.MaxMs), j))
		}
	}
	return strings.Join(L, "\n") + "\n"
}
