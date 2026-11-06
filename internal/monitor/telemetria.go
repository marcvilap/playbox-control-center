package monitor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"playbox/internal/adb"
	"playbox/internal/pc"
)

// vigilarTelemetria lee batería, pantalla y app de las gafas con telemetría
// activada y que responden en la red. Una llamada ADB por gafa y ciclo.
func (m *Monitor) vigilarTelemetria(ctx context.Context) {
	if m.RutaADB == "" && !m.TelemetriaSimulada {
		return
	}
	espera := 8 * time.Second // primera lectura en cuanto las gafas responden
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(espera):
		}
		m.leerTelemetria()
		espera = time.Duration(m.cfg.TelemetriaS) * time.Second
	}
}

// telemetriaSimulada: datos de ejemplo para el modo simulación.
func telemetriaSimulada(id string) adb.Telemetria {
	b, temp := 64+len(id)%3*9, 31.5
	t := adb.Telemetria{Leido: time.Now(), Pantalla: "Activa", App: "com.vex.arena"}
	switch id {
	case "PICO-02":
		t.App, t.EnMenu = "com.pvr.launcher", true
	case "PICO-05":
		b = 12
	case "PICO-08":
		b, t.Cargando = 41, true
	case "PICO-09":
		t.Pantalla = "Reposo"
	}
	t.BateriaPct, t.TempC = &b, &temp
	return t
}

func (m *Monitor) leerTelemetria() {
	type obj struct{ id, ip string }
	var lista []obj
	m.mu.Lock()
	for _, e := range m.equipos {
		if e.Tipo == "visor" && e.TelemetriaOn && e.Estado == Online {
			lista = append(lista, obj{e.ID, e.IP})
		}
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	lim := make(chan struct{}, 4)
	for _, o := range lista {
		wg.Add(1)
		go func(o obj) {
			defer wg.Done()
			lim <- struct{}{}
			defer func() { <-lim }()
			var tel adb.Telemetria
			if m.TelemetriaSimulada {
				tel = telemetriaSimulada(o.id)
			} else {
				tel = adb.LeerTelemetria(m.RutaADB, o.ip)
			}
			m.mu.Lock()
			defer m.mu.Unlock()
			if e := m.equipo(o.id); e != nil {
				e.Telemetria = &tel
				m.revisarTelemetria(e)
			}
		}(o)
	}
	wg.Wait()
}

// revisarTelemetria emite avisos una sola vez por batería baja o temperatura alta.
func (m *Monitor) revisarTelemetria(e *EstadoEquipo) {
	t := e.Telemetria
	if t == nil || t.Error != "" {
		return
	}
	if t.BateriaPct != nil {
		b := *t.BateriaPct
		baja := b < m.cfg.BateriaBajaPct && !t.Cargando
		if baja && !e.avisoBateria {
			e.avisoBateria = true
			m.caja.Registrar(Evento{Fuente: "telemetria", Dispositivo: e.ID, Tipo: "bateria_baja", Severidad: "aviso",
				Mensaje: fmt.Sprintf("%s: batería al %d %%", e.ID, b), Datos: map[string]any{"bateria_pct": b}})
		}
		if e.avisoBateria && (t.Cargando || b >= m.cfg.BateriaBajaPct+5) {
			e.avisoBateria = false
		}
	}
	if t.TempC != nil {
		alta := *t.TempC >= float64(m.cfg.TempAltaC)
		if alta && !e.avisoTemp {
			e.avisoTemp = true
			m.caja.Registrar(Evento{Fuente: "telemetria", Dispositivo: e.ID, Tipo: "temperatura_alta", Severidad: "aviso",
				Mensaje: fmt.Sprintf("%s: batería a %.1f °C", e.ID, *t.TempC), Datos: map[string]any{"temp_c": *t.TempC}})
		}
		if e.avisoTemp && *t.TempC < float64(m.cfg.TempAltaC)-3 {
			e.avisoTemp = false
		}
	}
}

// vigilarPC mide el PC: CPU/RAM/disco cada 10 s, GPU cada 30 s, teclado y
// ratón cada 2 min. Todo con llamadas ligeras.
func (m *Monitor) vigilarPC(ctx context.Context) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	var gpu *pc.GPU
	var entrada *pc.Entrada
	cpuAlta := 0
	for i := 0; ; i++ {
		s := pc.Leer()
		if i%3 == 0 {
			gpu = pc.LeerGPU()
		}
		if i%12 == 0 {
			if e := pc.LeerEntrada(); e != nil {
				entrada = e
			}
		}
		s.GPU, s.Entrada = gpu, entrada
		if s.CPUPct > 95 {
			cpuAlta++
		} else {
			cpuAlta = 0
		}
		m.mu.Lock()
		m.salud = &s
		m.avisoPC("cpu", cpuAlta >= 6, "aviso", fmt.Sprintf("CPU del PC por encima del 95 %% durante 1 minuto (%.0f %%)", s.CPUPct))
		m.avisoPC("ram", s.RAMPct > 90, "aviso", fmt.Sprintf("RAM del PC al %.0f %%", s.RAMPct))
		m.avisoPC("disco", s.DiscoLibreGB > 0 && s.DiscoLibreGB < 10, "aviso", fmt.Sprintf("Quedan %.1f GB libres en el disco C:", s.DiscoLibreGB))
		if gpu != nil {
			m.avisoPC("gpu", gpu.TempC >= 85, "aviso", fmt.Sprintf("Gráfica a %.0f °C", gpu.TempC))
		}
		if entrada != nil {
			sinEntrada := entrada.Teclados == 0 || entrada.Ratones == 0
			m.avisoPC("entrada", len(entrada.Problemas) > 0 || sinEntrada, "alta",
				fmt.Sprintf("Teclado o ratón con problemas (teclados %d, ratones %d, errores: %v)", entrada.Teclados, entrada.Ratones, entrada.Problemas))
		}
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// avisoPC registra un evento al entrar en una condición y otro al salir. Con m.mu tomado.
func (m *Monitor) avisoPC(clave string, activo bool, sev, msg string) {
	if m.avisosPC == nil {
		m.avisosPC = map[string]bool{}
	}
	switch {
	case activo && !m.avisosPC[clave]:
		m.avisosPC[clave] = true
		m.caja.Registrar(Evento{Fuente: "pc", Dispositivo: "PC", Tipo: "pc_" + clave, Severidad: sev, Mensaje: msg})
	case !activo && m.avisosPC[clave]:
		m.avisosPC[clave] = false
		m.caja.Registrar(Evento{Fuente: "pc", Dispositivo: "PC", Tipo: "pc_" + clave + "_ok", Severidad: "info", Mensaje: "PC: " + clave + " de nuevo normal"})
	}
}
