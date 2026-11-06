package adb

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"playbox/internal/ejec"
)

// Servicios del sistema que pueden contener datos de los mandos. Se buscan
// por nombre en «dumpsys -l»; solo se leen (dumpsys no cambia nada).
var reServicioMandos = regexp.MustCompile(`(?i)controller|pvr|pico|pxr|xr|vr|input|bluetooth|hid|battery|tracking|handle`)

// Diagnostico recoge, por USB y en solo lectura, la información necesaria
// para localizar la batería de los mandos. Devuelve la ruta del informe.
func Diagnostico(adb, serie, dirSalida string) (string, error) {
	sh := func(timeout time.Duration, args ...string) string {
		_, out := ejec.Run(timeout, adb, append([]string{"-s", serie, "shell"}, args...)...)
		return out
	}
	var b strings.Builder
	sec := func(titulo, contenido string) {
		fmt.Fprintf(&b, "\n===== %s =====\n%s\n", titulo, strings.TrimSpace(contenido))
	}
	fmt.Fprintf(&b, "Diagnóstico PlayBox · gafa %s · %s\n", serie, time.Now().Format("2006-01-02 15:04:05"))
	sec("modelo y firmware", sh(10*time.Second, "getprop | grep -iE 'ro.product|ro.build.display|ro.pico|ro.pvr|persist.pvr|controller'"))
	lista := sh(20*time.Second, "dumpsys", "-l")
	sec("dumpsys -l (servicios)", lista)
	sec("paquetes del sistema relacionados", sh(15*time.Second, "pm list packages | grep -iE 'pico|pvr|pxr|controller|bytedance'"))

	var candidatos []string
	for _, l := range strings.Split(lista, "\n") {
		s := strings.TrimSpace(l)
		if s != "" && !strings.Contains(s, " ") && reServicioMandos.MatchString(s) {
			candidatos = append(candidatos, s)
		}
	}
	sort.Strings(candidatos)
	if len(candidatos) > 40 {
		candidatos = candidatos[:40]
	}
	for _, s := range candidatos {
		out := sh(15*time.Second, "dumpsys", s)
		if len(out) > 60000 {
			out = out[:60000] + "\n[recortado]"
		}
		sec("dumpsys "+s, out)
	}
	if err := os.MkdirAll(dirSalida, 0o755); err != nil {
		return "", err
	}
	ruta := filepath.Join(dirSalida, fmt.Sprintf("diagnostico_gafa_%s_%s.txt",
		regexp.MustCompile(`[^\w.-]`).ReplaceAllString(serie, "_"), time.Now().Format("20060102_1504")))
	return ruta, os.WriteFile(ruta, []byte(b.String()), 0o644)
}
