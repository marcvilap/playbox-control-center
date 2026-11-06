package agentes

import (
	"fmt"
	"strings"
	"time"

	"playbox/internal/ejec"
)

// NombreRegla es la regla del Firewall de Windows que deja entrar a los agentes.
const NombreRegla = "PlayBox agentes de gafas"

func argsRegla() string {
	return fmt.Sprintf(`advfirewall firewall add rule name="%s" dir=in action=allow protocol=TCP localport=%d profile=any`, NombreRegla, Puerto)
}

// ReglaFirewall indica si ya existe la regla (no necesita permisos de administrador).
func ReglaFirewall() bool {
	if !ejec.EsWindows() {
		return true
	}
	code, out := ejec.Run(10*time.Second, "netsh", "advfirewall", "firewall", "show", "rule", "name="+NombreRegla)
	return code == 0 && strings.Contains(out, NombreRegla)
}

// CrearReglaDirecta crea la regla (hay que ser administrador: instalación del servicio).
func CrearReglaDirecta() error {
	if !ejec.EsWindows() || ReglaFirewall() {
		return nil
	}
	code, out := ejec.Run(15*time.Second, "netsh", "advfirewall", "firewall", "add", "rule", "name="+NombreRegla,
		"dir=in", "action=allow", "protocol=TCP", fmt.Sprintf("localport=%d", Puerto), "profile=any")
	if code != 0 {
		return fmt.Errorf("netsh: %s", strings.TrimSpace(ejec.Stdout(out)))
	}
	return nil
}

// CrearReglaElevada pide permiso de administrador (ventana de Windows) y crea la regla.
func CrearReglaElevada() error {
	if !ejec.EsWindows() || ReglaFirewall() {
		return nil
	}
	script := fmt.Sprintf(`Start-Process -FilePath netsh -Verb RunAs -WindowStyle Hidden -Wait -ArgumentList '%s'`,
		strings.ReplaceAll(argsRegla(), "'", "''"))
	code, out := ejec.PS(120*time.Second, script)
	if code != 0 {
		return fmt.Errorf("no se pudo crear la regla (¿se canceló el permiso?): %s", strings.TrimSpace(ejec.Stdout(out)))
	}
	if !ReglaFirewall() {
		return fmt.Errorf("la regla no aparece en el firewall")
	}
	return nil
}
