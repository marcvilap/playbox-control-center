package adb

import (
	"fmt"
	"strings"
	"time"

	"playbox/internal/ejec"
)

// PaqueteAgente es el Agente PlayBox (agente-gafas).
const PaqueteAgente = "com.playboxvr.agente"

// Destino es el serie USB si hay, o «ip:5555» por Wi-Fi.
func Destino(serie, ip string, porUSB bool) string {
	if porUSB && serie != "" {
		return serie
	}
	if ip != "" {
		return ip + ":" + PuertoWifi
	}
	return serie
}

// AgenteInstalado indica si la gafa tiene el Agente PlayBox y su versión.
func AgenteInstalado(adb, destino string) (bool, string) {
	code, out := ejec.Run(15*time.Second, adb, "-s", destino, "shell", "dumpsys package "+PaqueteAgente+" | grep -m1 versionName=")
	if code != 0 {
		return false, ""
	}
	o := strings.TrimSpace(ejec.Stdout(out))
	if i := strings.Index(o, "versionName="); i >= 0 {
		return true, strings.TrimSpace(o[i+len("versionName="):])
	}
	return false, ""
}

// ConfigurarAgente apunta el agente a este PC (url http://IP:47334/) con la
// clave de alta local. El receptor del agente exige el permiso DUMP: solo adb
// puede hacerlo. El agente se vuelve a dar de alta solo.
func ConfigurarAgente(adb, destino, url, clave, serie, nombre string) error {
	for _, v := range []string{url, clave, serie, nombre} {
		if strings.ContainsAny(v, " '\"`$;&|<>\\") {
			return fmt.Errorf("valor no válido para la gafa: %q", v)
		}
	}
	cmd := "am broadcast -a " + PaqueteAgente + ".CONFIGURE -n " + PaqueteAgente + "/.ConfigReceiver --es url " + url + " --es key " + clave
	if serie != "" {
		cmd += " --es serial " + serie
	}
	if nombre != "" {
		cmd += " --es name " + nombre
	}
	code, out := ejec.Run(20*time.Second, adb, "-s", destino, "shell", cmd)
	o := ejec.Stdout(out)
	if code != 0 || !strings.Contains(o, "result=") {
		return fmt.Errorf("la gafa no aceptó la configuración: %s", strings.TrimSpace(o))
	}
	if !strings.Contains(o, `data="ok"`) {
		return fmt.Errorf("el agente no respondió (¿versión antigua?): %s", strings.TrimSpace(o))
	}
	return nil
}
