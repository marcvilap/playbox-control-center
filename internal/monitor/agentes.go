package monitor

import (
	"context"
	"fmt"
	"time"

	"playbox/internal/agentes"
)

// AgenteVisor es lo que el Agente PlayBox de la gafa cuenta (vía red local).
type AgenteVisor struct {
	agentes.Gafa
	Fresco bool `json:"fresco"`
}

// Agentes devuelve el receptor de agentes si este proceso escucha el puerto
// (nil si lo escucha otro proceso, p. ej. el servicio, o está desactivado).
func (m *Monitor) Agentes() *agentes.Servidor {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ag
}

// ReceptorAgentes describe dónde se reciben los agentes, para la ventana.
func (m *Monitor) ReceptorAgentes() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.agEstado
}

func (m *Monitor) arrancarAgentes(ctx context.Context) {
	if m.PuertoAgentes <= 0 {
		return
	}
	if m.TelemetriaSimulada {
		m.mu.Lock()
		m.agEstado = "simulado"
		m.mu.Unlock()
		go m.vigilarAgentes(ctx)
		return
	}
	srv, err := agentes.Nuevo(m.dir)
	estado := fmt.Sprintf("escuchando en el puerto %d", m.PuertoAgentes)
	if err != nil {
		estado = "error: " + err.Error()
		srv = nil
	} else {
		srv.OnEvento = func(gafa, tipo, sev, msg string, datos map[string]any) {
			m.caja.Registrar(Evento{Fuente: "agente", Dispositivo: m.idDeGafa(gafa), Tipo: tipo, Severidad: sev,
				Mensaje: msg, Fiabilidad: "observado", Datos: datos})
		}
		if err := srv.Escuchar(m.PuertoAgentes); err != nil {
			// Otro proceso (el servicio) ya los escucha: aquí solo se lee su registro.
			estado, srv = "otro proceso", nil
		}
	}
	m.mu.Lock()
	m.ag, m.agEstado = srv, estado
	m.mu.Unlock()
	go m.vigilarAgentes(ctx)
}

// cerrarAgentes suelta el puerto antes de que Ejecutar termine (un monitor
// nuevo lo vuelve a abrir enseguida).
func (m *Monitor) cerrarAgentes() {
	m.mu.Lock()
	srv := m.ag
	m.ag = nil
	m.mu.Unlock()
	if srv != nil {
		srv.Cerrar()
	}
}

// idDeGafa traduce el nombre que dio el agente al ID del visor (si coincide).
func (m *Monitor) idDeGafa(nombre string) string {
	for _, v := range m.cfg.Visores {
		if v.ID == nombre {
			return v.ID
		}
	}
	return nombre
}

func (m *Monitor) vigilarAgentes(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		m.leerAgentes()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (m *Monitor) leerAgentes() {
	var gafas []agentes.Gafa
	m.mu.Lock()
	srv := m.ag
	m.mu.Unlock()
	switch {
	case m.TelemetriaSimulada:
		gafas = m.agentesSimulados()
	case srv != nil:
		gafas = srv.Gafas()
	default:
		gafas = agentes.GafasGuardadas(m.dir)
	}
	ahora := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.equipos {
		if e.Tipo != "visor" {
			continue
		}
		g, porIdentidad := m.emparejar(e, gafas)
		if g == nil {
			e.Agente = nil
			continue
		}
		e.Agente = &AgenteVisor{Gafa: *g, Fresco: g.Fresca(ahora)}
		// El agente dice desde qué IP habla: si la gafa cambió de IP, se sigue.
		ip := g.Estado.WifiIP
		if ip == "" {
			ip = g.IP
		}
		if porIdentidad && e.AgenteFresco() && ip != "" && ip != e.IP && e.IP != "" {
			ant := e.IP
			e.IP = ip
			m.caja.Registrar(Evento{Fuente: "agente", Dispositivo: e.ID, Tipo: "ip_cambiada", Severidad: "info",
				Mensaje: fmt.Sprintf("%s ahora está en %s (antes %s), según su agente", e.ID, ip, ant), Fiabilidad: "observado",
				Datos: map[string]any{"ip_anterior": ant, "ip_nueva": ip}})
			if m.OnIPCambiada != nil {
				go m.OnIPCambiada(e.ID, ip)
			}
		}
	}
}

// AgenteFresco: hay datos recientes del agente.
func (e *EstadoEquipo) AgenteFresco() bool { return e.Agente != nil && e.Agente.Fresco }

// emparejar busca la gafa del agente de un visor: por número de serie, por
// nombre (el agente se configura con el ID del visor) o, si no, por IP.
func (m *Monitor) emparejar(e *EstadoEquipo, gafas []agentes.Gafa) (*agentes.Gafa, bool) {
	serie := ""
	for _, v := range m.cfg.Visores {
		if v.ID == e.ID {
			serie = v.Serie
		}
	}
	for i := range gafas {
		if serie != "" && gafas[i].Serie == serie {
			return &gafas[i], true
		}
	}
	for i := range gafas {
		if gafas[i].Nombre == e.ID {
			return &gafas[i], true
		}
	}
	for i := range gafas {
		if e.IP != "" && (gafas[i].IP == e.IP || gafas[i].Estado.WifiIP == e.IP) {
			return &gafas[i], false
		}
	}
	return nil, false
}

func (m *Monitor) agentesSimulados() []agentes.Gafa {
	n := func(v int) *int { return &v }
	f := func(v float64) *float64 { return &v }
	on := true
	var out []agentes.Gafa
	for i, v := range m.cfg.Visores {
		if v.IP == "" || i%2 == 1 {
			continue // la mitad sin agente, para ver los dos casos
		}
		izq, der := n(4), n(3)
		if v.ID == "PICO-05" {
			izq = n(1)
		}
		out = append(out, agentes.Gafa{ID: int64(i + 1), Nombre: v.ID, Serie: "SIM" + v.ID, Modelo: "PICO A9210",
			IP: v.IP, Version: "0.39", Codigo: 39, Contacto: time.Now(),
			Estado: agentes.Estado{BateriaPct: n(80 - i*5), TempC: f(33.5), PantallaOn: &on, WifiSSID: "PlayBox",
				WifiRSSI: n(-52 - i), App: "com.vexsolutions.arena", AppNombre: "VEX Arena", MandoIzq: izq, MandoDer: der,
				MandosConectados: n(3), SDKListo: true}})
	}
	return out
}
