package monitor

import (
	"fmt"
	"time"
)

// Hipotesis con evidencias y nivel de confianza (spec §8). Nunca es una causa confirmada.
type Hipotesis struct {
	Texto            string   `json:"texto"`
	Confianza        string   `json:"confianza"` // baja, media, alta
	Evidencias       []string `json:"evidencias"`
	Contraevidencias []string `json:"contraevidencias,omitempty"`
	Validacion       []string `json:"pasos_validacion"`
}

// Expediente es el archivo que se guarda por incidente.
type Expediente struct {
	ID             string         `json:"id"`
	Tipo           string         `json:"tipo"`
	Severidad      string         `json:"severidad"`
	Dispositivo    string         `json:"dispositivo"`
	Resumen        string         `json:"resumen"`
	Inicio         time.Time      `json:"inicio"`
	VentanaDesde   time.Time      `json:"ventana_desde"`
	VentanaHasta   time.Time      `json:"ventana_hasta"`
	Recuperado     bool           `json:"recuperado_al_cerrar"`
	EstadoAlInicio EstadoGlobal   `json:"estado_al_inicio"`
	EstadoAlCierre EstadoGlobal   `json:"estado_al_cierre"`
	Hipotesis      []Hipotesis    `json:"hipotesis"`
	Eventos        []Evento       `json:"eventos"`
	Muestras       []Muestra      `json:"muestras"`
	Datos          map[string]any `json:"datos"`
	Nota           string         `json:"nota"`
}

func (m *Monitor) abrirIncidente(e *EstadoEquipo, tipo, sev, resumen string, datos map[string]any, ahora time.Time) {
	id := fmt.Sprintf("%s-%s-%s", ahora.UTC().Format("20060102T150405Z"), tipo, e.ID)
	dia := ahora.Format("2006-01-02")
	if dia != m.diaInc {
		m.diaInc, m.incHoy = dia, 0
	}
	m.incHoy++
	m.ultimoInc = id
	m.caja.Registrar(Evento{TS: ahora, Fuente: "monitor", Dispositivo: e.ID, Tipo: tipo, Severidad: sev,
		Mensaje: resumen, Incidente: id, Datos: datos})
	m.log(fmt.Sprintf("[%s] %s", sev, resumen))

	exp := &Expediente{ID: id, Tipo: tipo, Severidad: sev, Dispositivo: e.ID, Resumen: resumen, Inicio: ahora.UTC(),
		EstadoAlInicio: m.estadoLocked(), Hipotesis: m.hipotesis(tipo, e), Datos: datos,
		Nota: "Hipótesis generadas por reglas deterministas a partir de la conectividad observada; no son causas confirmadas."}
	antes := time.Duration(m.cfg.VentanaAntesMin) * time.Minute
	despues := time.Duration(m.cfg.VentanaDespuesMin) * time.Minute
	m.pendientes.Add(1)
	go func() {
		defer m.pendientes.Done()
		// Se guarda aunque el fallo se resuelva solo; si se para el monitor,
		// se guarda en el acto con lo que haya (el servicio debe parar rápido).
		if m.ctx != nil {
			select {
			case <-time.After(despues):
			case <-m.ctx.Done():
			}
		} else {
			time.Sleep(despues)
		}
		m.cerrarIncidente(exp, e.ID, ahora.Add(-antes), time.Now())
	}()
}

func (m *Monitor) cerrarIncidente(exp *Expediente, disp string, desde, hasta time.Time) {
	exp.VentanaDesde, exp.VentanaHasta = desde.UTC(), hasta.UTC()
	exp.Muestras, exp.Eventos = m.caja.Ventana(desde, hasta)
	m.mu.Lock()
	exp.EstadoAlCierre = m.estadoLocked()
	if e := m.equipo(disp); e != nil {
		exp.Recuperado = e.Estado == Online && !e.LatenciaAlta
	}
	m.mu.Unlock()
	if ruta, err := m.caja.GuardarExpediente(exp.ID, exp); err == nil {
		m.log("Expediente guardado: " + ruta)
	}
}

func (m *Monitor) hipotesis(tipo string, e *EstadoEquipo) []Hipotesis {
	routerOK := false
	if r := m.equipo("ROUTER"); r != nil {
		routerOK = r.Estado == Online
	}
	tendencia := m.tendencia(e)
	switch tipo {
	case "visor_perdido_en_uso":
		contra := []string{}
		if routerOK {
			contra = append(contra, "El router seguía respondiendo: no parece una caída general de la red")
		}
		return []Hipotesis{
			{Texto: "El visor se apagó, entró en reposo o se quedó sin batería", Confianza: "baja",
				Evidencias: []string{fmt.Sprintf("%d visores seguían activos", m.visoresOnline())},
				Validacion: []string{"Mirar batería y pantalla del visor", "Telemetría de batería y reposo: pendiente (agente/ADB)"}},
			{Texto: "El visor perdió la Wi-Fi (cobertura, interferencias o cambio de banda)", Confianza: "baja",
				Evidencias: []string{tendencia}, Contraevidencias: contra,
				Validacion: []string{"Revisar en el router si el visor sigue en la lista de clientes", "Comparar con la posición del visor en la sala"}},
		}
	case "router_sin_respuesta":
		return []Hipotesis{
			{Texto: "Router colgado o reiniciándose", Confianza: "media", Evidencias: []string{"El PC no recibe respuesta del router"},
				Validacion: []string{"Mirar las luces del router", "Comprobar si los visores también han caído"}},
			{Texto: "Cable o puerto Ethernet del PC", Confianza: "media", Evidencias: []string{"Sin datos del adaptador en esta versión"},
				Validacion: []string{"Revisar el cable y el icono de red de Windows"}},
		}
	case "internet_caido_lan_ok":
		return []Hipotesis{{Texto: "Caída de la conexión del operador", Confianza: "media",
			Evidencias: []string{"El router responde pero el destino externo no"},
			Validacion: []string{"Abrir una web desde el PC", "Ojo: juegos con licencia en línea pueden fallar al arrancar"}}}
	case "latencia_alta":
		return []Hipotesis{{Texto: "Congestión o interferencias Wi-Fi, o visor lejos del router", Confianza: "baja",
			Evidencias: []string{tendencia}, Validacion: []string{"Comparar con la latencia de los otros visores en el mismo momento"}}}
	}
	return nil
}

func (m *Monitor) tendencia(e *EstadoEquipo) string {
	if len(e.hist) == 0 {
		return "Sin muestras previas"
	}
	ok, suma := 0, 0.0
	for _, h := range e.hist {
		if h.OK {
			ok++
			suma += h.RTTms
		}
	}
	if ok == 0 {
		return fmt.Sprintf("Ninguna de las últimas %d muestras respondió", len(e.hist))
	}
	return fmt.Sprintf("Últimas %d muestras: %d respondieron, latencia media %.0f ms", len(e.hist), ok, suma/float64(ok))
}
