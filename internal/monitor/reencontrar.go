package monitor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"playbox/internal/red"
)

// reencontrar: si una gafa vinculada (con MAC) deja de responder, la busca
// por su MAC en la red por si el router le ha dado otra IP. Solo lee.
func (m *Monitor) reencontrar(ctx context.Context) {
	var ultimoBarrido time.Time
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		perdidas := m.gafasPerdidas(60 * time.Second)
		if len(perdidas) == 0 {
			continue
		}
		pendientes := m.aplicarTabla(red.TablaARP(), perdidas)
		if len(pendientes) == 0 || time.Since(ultimoBarrido) < 5*time.Minute {
			continue
		}
		ultimoBarrido = time.Now()
		c, cancel := context.WithTimeout(ctx, 60*time.Second)
		res, err := red.Escanear(c, m.NuevaSonda, "")
		cancel()
		if err != nil {
			continue
		}
		tabla := map[string]string{}
		for _, d := range res.Dispositivos {
			if d.MAC != "" {
				tabla[d.IP] = d.MAC
			}
		}
		m.aplicarTabla(tabla, pendientes)
	}
}

// gafasPerdidas devuelve los IDs de gafas con MAC que llevan sin responder más de «tras».
func (m *Monitor) gafasPerdidas(tras time.Duration) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var ids []string
	for _, e := range m.equipos {
		if e.Tipo == "visor" && e.MAC != "" && e.Estado != Online && time.Since(e.Desde) > tras {
			ids = append(ids, e.ID)
		}
	}
	return ids
}

// aplicarTabla actualiza la IP de las gafas cuya MAC aparece en la tabla
// ip→mac con otra IP. Devuelve las que siguen sin encontrarse.
func (m *Monitor) aplicarTabla(tabla map[string]string, ids []string) []string {
	porMAC := map[string]string{}
	for ip, mac := range tabla {
		porMAC[strings.ToLower(mac)] = ip
	}
	var pendientes []string
	for _, id := range ids {
		m.mu.Lock()
		e := m.equipo(id)
		if e == nil {
			m.mu.Unlock()
			continue
		}
		nueva, ok := porMAC[strings.ToLower(e.MAC)]
		if !ok || nueva == e.IP {
			m.mu.Unlock()
			pendientes = append(pendientes, id)
			continue
		}
		vieja := e.IP
		e.IP, e.fallos, e.hist = nueva, 0, nil
		m.cambiar(e, SinConf, time.Now())
		m.caja.Registrar(Evento{Fuente: "monitor", Dispositivo: id, Tipo: "ip_cambiada", Severidad: "aviso",
			Mensaje: fmt.Sprintf("%s ha cambiado de IP: %s → %s (encontrada por su MAC)", id, vieja, nueva),
			Datos:   map[string]any{"ip_anterior": vieja, "ip_nueva": nueva, "mac": e.MAC}})
		cb := m.OnIPCambiada
		m.mu.Unlock()
		if cb != nil {
			cb(id, nueva)
		}
	}
	return pendientes
}
