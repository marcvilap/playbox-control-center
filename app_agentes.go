package main

import (
	"fmt"
	"net/http"
	"time"

	"playbox/internal/adb"
	"playbox/internal/agentes"
	"playbox/internal/monitor"
)

// firewallOK comprueba (como mucho cada minuto) la regla del firewall.
func (a *App) firewallOK() bool {
	a.mu.Lock()
	if time.Since(a.firewall.leido) < time.Minute {
		ok := a.firewall.ok
		a.mu.Unlock()
		return ok
	}
	a.mu.Unlock()
	ok := agentes.ReglaFirewall()
	a.mu.Lock()
	a.firewall.ok, a.firewall.leido = ok, time.Now()
	a.mu.Unlock()
	return ok
}

// claveAgentes: la del receptor de este proceso o, si lo tiene el servicio, la de su registro.
func (a *App) claveAgentes() (string, error) {
	a.mu.Lock()
	mon, dir := a.mon, a.dirMon
	a.mu.Unlock()
	if mon != nil {
		if srv := mon.Agentes(); srv != nil {
			return srv.ClaveAlta(), nil
		}
	}
	if dir == "" {
		dir = dirDatos()
	}
	srv, err := agentes.Nuevo(dir) // crea la clave si aún no existe (no abre el puerto)
	if err != nil {
		return "", err
	}
	return srv.ClaveAlta(), nil
}

// apiAgenteConectar apunta el Agente PlayBox de una gafa vinculada a este PC,
// por USB si está enchufada o, si no, por ADB Wi-Fi.
func (a *App) apiAgenteConectar(r *http.Request) (any, error) {
	var in struct {
		ID string `json:"id"`
	}
	if err := leerJSON(r, &in); err != nil || in.ID == "" {
		return nil, fmt.Errorf("falta la gafa")
	}
	ruta := adb.Localizar(dirExe())
	if ruta == "" {
		return nil, fmt.Errorf("falta la herramienta USB: descárgala desde «Vincular gafas por USB»")
	}
	cfg, _, err := monitor.Cargar(rutaConfig())
	if err != nil {
		return nil, err
	}
	var v *monitor.Equipo
	for i := range cfg.Visores {
		if cfg.Visores[i].ID == in.ID {
			v = &cfg.Visores[i]
		}
	}
	if v == nil || v.IP == "" {
		return nil, fmt.Errorf("vincula primero %s por USB", in.ID)
	}
	porUSB := false
	if gs, err := adb.Dispositivos(ruta); err == nil {
		for _, g := range gs {
			porUSB = porUSB || (g.Serie == v.Serie && g.Estado == "device")
		}
	}
	destino := adb.Destino(v.Serie, v.IP, porUSB)
	if !porUSB {
		if err := adb.Conectar(ruta, v.IP); err != nil {
			return nil, fmt.Errorf("enchufa la gafa por USB (o activa antes «batería y app»): %v", err)
		}
	}
	instalado, version := adb.AgenteInstalado(ruta, destino)
	if !instalado {
		return nil, fmt.Errorf("%s no tiene el Agente PlayBox instalado (llegará en la próxima versión desde aquí)", in.ID)
	}
	clave, err := a.claveAgentes()
	if err != nil {
		return nil, err
	}
	ipPC := agentes.IPLocalPara(v.IP)
	if ipPC == "" {
		return nil, fmt.Errorf("este PC no tiene IP en la red de la gafa")
	}
	url := fmt.Sprintf("http://%s:%d/", ipPC, agentes.Puerto)
	if err := adb.ConfigurarAgente(ruta, destino, url, clave, v.Serie, v.ID); err != nil {
		return nil, err
	}
	a.mu.Lock()
	mon := a.mon
	a.mu.Unlock()
	if mon != nil {
		mon.Caja().Registrar(monitor.Evento{Fuente: "playbox", Dispositivo: v.ID, Tipo: "agente_configurado", Severidad: "info",
			Mensaje: fmt.Sprintf("Agente de %s apuntado a este PC (%s)", v.ID, url), Fiabilidad: "observado",
			Datos: map[string]any{"url": url, "version_agente": version, "por_usb": porUSB}})
	}
	return map[string]any{"id": v.ID, "url": url, "version": version, "firewall": a.firewallOK()}, nil
}
