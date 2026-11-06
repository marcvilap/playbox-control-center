// Package monitor vigila en continuo visores, router e Internet, guarda la
// caja negra y abre expedientes de incidente. Solo observa: nunca actúa
// sobre visores, juegos ni red.
package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"playbox/internal/ejec"
	"playbox/internal/sonda"
)

// Estados visibles (spec §9). Esta versión solo puede observar conectividad.
const (
	Online     = "ONLINE"
	Offline    = "OFFLINE"
	SinConf    = "SIN CONFIRMACIÓN"
	SinConfig  = "SIN CONFIGURAR"
	histMax    = 12 // muestras recientes por equipo
	ventanaLat = 6
)

// EstadoEquipo es el estado de un equipo con su fuente y frescura.
type EstadoEquipo struct {
	ID             string     `json:"id"`
	Tipo           string     `json:"tipo"` // visor, router, internet
	IP             string     `json:"ip"`
	Estado         string     `json:"estado"`
	Fuente         string     `json:"fuente"`
	Fiabilidad     string     `json:"fiabilidad"`
	Desde          time.Time  `json:"desde"`
	UltimoContacto *time.Time `json:"ultimo_contacto"`
	RTTms          *float64   `json:"rtt_ms"`
	PerdidaPct     float64    `json:"perdida_1min_pct"`
	JitterMs       *float64   `json:"jitter_ms"`
	LatenciaAlta   bool       `json:"latencia_alta"`

	fallos int
	hist   []Muestra
	sonda  sonda.Sonda
}

// EstadoGlobal se escribe en estado.json en cada ciclo.
type EstadoGlobal struct {
	Version         string         `json:"version"`
	Actualizado     time.Time      `json:"actualizado"`
	Inicio          time.Time      `json:"inicio"`
	IntervaloS      int            `json:"intervalo_s"`
	Visores         []EstadoEquipo `json:"visores"`
	Router          EstadoEquipo   `json:"router"`
	Internet        EstadoEquipo   `json:"internet"`
	VisoresOnline   int            `json:"visores_online"`
	CentroActivo    bool           `json:"centro_activo"`
	IncidentesHoy   int            `json:"incidentes_hoy"`
	UltimoIncidente string         `json:"ultimo_incidente,omitempty"`
}

// Monitor coordina la vigilancia.
type Monitor struct {
	cfg        Config
	dir        string
	version    string
	caja       *Caja
	log        func(string)
	NuevaSonda func() (sonda.Sonda, error)

	mu           sync.Mutex
	equipos      []*EstadoEquipo
	inicio       time.Time
	ultimoOnline time.Time
	incHoy       int
	diaInc       string
	ultimoInc    string
	pendientes   sync.WaitGroup
	ctx          context.Context
}

// Nuevo prepara el monitor; dir es la carpeta de datos.
func Nuevo(cfg Config, dir, version string, log func(string)) (*Monitor, error) {
	caja, err := NuevaCaja(dir, cfg.RetencionDias, time.Duration(cfg.VentanaAntesMin+cfg.VentanaDespuesMin+3)*time.Minute)
	if err != nil {
		return nil, err
	}
	if log == nil {
		log = func(string) {}
	}
	return &Monitor{cfg: cfg, dir: dir, version: version, caja: caja, log: log, NuevaSonda: sonda.Nueva}, nil
}

// Caja da acceso a la caja negra (para eventos externos).
func (m *Monitor) Caja() *Caja { return m.caja }

func (m *Monitor) prepararEquipos() error {
	ahora := time.Now()
	add := func(id, tipo, ip string) error {
		e := &EstadoEquipo{ID: id, Tipo: tipo, IP: ip, Estado: SinConf, Fuente: "ICMP", Fiabilidad: "observado", Desde: ahora}
		if ip == "" {
			e.Estado, e.Fuente = SinConfig, ""
		} else {
			s, err := m.NuevaSonda()
			if err != nil {
				return err
			}
			e.sonda = s
		}
		m.equipos = append(m.equipos, e)
		return nil
	}
	for _, v := range m.cfg.Visores {
		if err := add(v.ID, "visor", v.IP); err != nil {
			return err
		}
	}
	router := m.cfg.Router
	if router == "" {
		router = PuertaEnlace()
	}
	if err := add("ROUTER", "router", router); err != nil {
		return err
	}
	return add("INTERNET", "internet", m.cfg.Internet)
}

// Ejecutar vigila hasta que se cancela ctx.
func (m *Monitor) Ejecutar(ctx context.Context) error {
	m.inicio = time.Now()
	m.ctx = ctx
	if err := m.prepararEquipos(); err != nil {
		return err
	}
	defer func() {
		for _, e := range m.equipos {
			if e.sonda != nil {
				e.sonda.Cerrar()
			}
		}
		m.pendientes.Wait()
		m.caja.Registrar(Evento{Fuente: "playbox", Tipo: "monitor_parado", Severidad: "info", Mensaje: "Vigilancia detenida"})
		m.caja.Cerrar()
	}()
	m.caja.Registrar(Evento{Fuente: "playbox", Tipo: "monitor_iniciado", Severidad: "info",
		Mensaje: fmt.Sprintf("Vigilancia iniciada (v%s, %d visores con IP)", m.version, m.cfg.VisoresConIP()),
		Datos:   map[string]any{"version": m.version, "intervalo_s": m.cfg.IntervaloS}})
	m.log(fmt.Sprintf("Vigilando %d visores, router e Internet cada %d s", m.cfg.VisoresConIP(), m.cfg.IntervaloS))

	go m.vigilarWindows(ctx)
	tick := time.NewTicker(time.Duration(m.cfg.IntervaloS) * time.Second)
	defer tick.Stop()
	purga := time.NewTicker(6 * time.Hour)
	defer purga.Stop()
	m.ciclo()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			m.ciclo()
		case <-purga.C:
			m.caja.Purgar()
		}
	}
}

// Activo indica si el centro parece estar en uso (algún visor visto en 10 min).
func (m *Monitor) Activo() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return time.Since(m.ultimoOnline) < 10*time.Minute
}

// Instantanea devuelve una copia del estado actual.
func (m *Monitor) Instantanea() EstadoGlobal {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.estadoLocked()
}

func (m *Monitor) ciclo() {
	espera := time.Duration(m.cfg.EsperaMs) * time.Millisecond
	res := make([]sonda.Resultado, len(m.equipos))
	var wg sync.WaitGroup
	for i, e := range m.equipos {
		if e.sonda == nil {
			continue
		}
		wg.Add(1)
		go func(i int, e *EstadoEquipo) {
			defer wg.Done()
			res[i] = e.sonda.Eco(e.IP, espera)
		}(i, e)
	}
	wg.Wait()
	ahora := time.Now()
	m.mu.Lock()
	for i, e := range m.equipos {
		if e.sonda != nil {
			m.procesar(e, res[i], ahora)
		}
	}
	est := m.estadoLocked()
	m.mu.Unlock()
	m.escribirEstado(est)
}

func (m *Monitor) procesar(e *EstadoEquipo, r sonda.Resultado, ahora time.Time) {
	mu := Muestra{TS: ahora, Disp: e.ID, OK: r.OK}
	if r.OK {
		mu.RTTms = float64(r.RTT.Microseconds()) / 1000
	}
	m.caja.Anotar(mu)
	e.hist = append(e.hist, mu)
	if len(e.hist) > histMax {
		e.hist = e.hist[len(e.hist)-histMax:]
	}
	e.PerdidaPct, e.JitterMs = estadisticas(e.hist)

	if r.OK {
		t, rtt := ahora, mu.RTTms
		e.UltimoContacto, e.RTTms, e.fallos = &t, &rtt, 0
		if e.Tipo == "visor" {
			m.ultimoOnline = ahora
		}
		switch e.Estado {
		case SinConf:
			m.cambiar(e, Online, ahora)
			m.caja.Registrar(Evento{Fuente: "monitor", Dispositivo: e.ID, Tipo: "visible", Severidad: "info",
				Mensaje: e.ID + " responde", Datos: map[string]any{"ip": e.IP, "rtt_ms": rtt}})
		case Offline:
			fuera := ahora.Sub(e.Desde).Round(time.Second)
			m.cambiar(e, Online, ahora)
			m.caja.Registrar(Evento{Fuente: "monitor", Dispositivo: e.ID, Tipo: "recuperado", Severidad: "info",
				Mensaje: fmt.Sprintf("%s vuelve a responder tras %s", e.ID, fuera),
				Datos:   map[string]any{"ip": e.IP, "segundos_sin_respuesta": fuera.Seconds()}})
		}
		m.revisarLatencia(e, ahora)
		return
	}
	e.fallos++
	if e.fallos >= m.cfg.FallosParaOffline && e.Estado != Offline {
		previo := e.Estado
		m.cambiar(e, Offline, ahora)
		e.RTTms, e.LatenciaAlta = nil, false
		if previo == Online {
			m.alPerder(e, ahora, r.Err)
		} else {
			m.caja.Registrar(Evento{Fuente: "monitor", Dispositivo: e.ID, Tipo: "no_visible", Severidad: "info",
				Mensaje: e.ID + " no responde desde el arranque", Datos: map[string]any{"ip": e.IP}})
		}
	}
}

func (m *Monitor) cambiar(e *EstadoEquipo, estado string, ahora time.Time) {
	e.Estado, e.Desde = estado, ahora
}

func (m *Monitor) visoresOnline() int {
	n := 0
	for _, e := range m.equipos {
		if e.Tipo == "visor" && e.Estado == Online {
			n++
		}
	}
	return n
}

func (m *Monitor) equipo(id string) *EstadoEquipo {
	for _, e := range m.equipos {
		if e.ID == id {
			return e
		}
	}
	return nil
}

func (m *Monitor) alPerder(e *EstadoEquipo, ahora time.Time, causa string) {
	datos := map[string]any{"ip": e.IP, "error": causa, "fallos_seguidos": e.fallos}
	switch e.Tipo {
	case "visor":
		otros := m.visoresOnline()
		datos["otros_visores_online"] = otros
		if otros == 0 {
			// Todos fuera: lo normal al cerrar o apagar visores. Sin expediente.
			m.caja.Registrar(Evento{Fuente: "monitor", Dispositivo: e.ID, Tipo: "sin_respuesta", Severidad: "info",
				Fiabilidad: "inferido", Mensaje: e.ID + " deja de responder; ningún otro visor activo (probable apagado)", Datos: datos})
			return
		}
		m.abrirIncidente(e, "visor_perdido_en_uso", "alta",
			fmt.Sprintf("%s deja de responder mientras %d visores siguen activos", e.ID, otros), datos, ahora)
	case "router":
		m.abrirIncidente(e, "router_sin_respuesta", "critica", "El router no responde al PC", datos, ahora)
	case "internet":
		if r := m.equipo("ROUTER"); r != nil && r.Estado == Online {
			m.abrirIncidente(e, "internet_caido_lan_ok", "aviso", "Sin Internet; la red local funciona", datos, ahora)
		} else {
			m.caja.Registrar(Evento{Fuente: "monitor", Dispositivo: e.ID, Tipo: "sin_respuesta", Severidad: "aviso",
				Mensaje: "Sin Internet (el router tampoco responde)", Datos: datos})
		}
	}
}

func (m *Monitor) revisarLatencia(e *EstadoEquipo, ahora time.Time) {
	if e.Tipo == "internet" {
		return // la latencia a Internet no afecta a las partidas locales
	}
	altas, n := 0, 0
	for i := len(e.hist) - 1; i >= 0 && n < ventanaLat; i-- {
		if e.hist[i].OK {
			n++
			if e.hist[i].RTTms > float64(m.cfg.LatenciaAltaMs) {
				altas++
			}
		}
	}
	switch {
	case !e.LatenciaAlta && altas >= 3:
		e.LatenciaAlta = true
		m.abrirIncidente(e, "latencia_alta", "aviso",
			fmt.Sprintf("%s: %d de las últimas %d respuestas superan %d ms", e.ID, altas, n, m.cfg.LatenciaAltaMs),
			map[string]any{"ip": e.IP, "umbral_ms": m.cfg.LatenciaAltaMs}, ahora)
	case e.LatenciaAlta && altas <= 1:
		e.LatenciaAlta = false
		m.caja.Registrar(Evento{Fuente: "monitor", Dispositivo: e.ID, Tipo: "latencia_normal", Severidad: "info",
			Mensaje: e.ID + ": latencia de nuevo normal"})
	}
}

func estadisticas(h []Muestra) (float64, *float64) {
	if len(h) == 0 {
		return 0, nil
	}
	perdidas := 0
	var rtts []float64
	for _, x := range h {
		if x.OK {
			rtts = append(rtts, x.RTTms)
		} else {
			perdidas++
		}
	}
	perdida := math.Round(float64(perdidas)/float64(len(h))*1000) / 10
	if len(rtts) < 2 {
		return perdida, nil
	}
	var s float64
	for i := 1; i < len(rtts); i++ {
		s += math.Abs(rtts[i] - rtts[i-1])
	}
	j := math.Round(s/float64(len(rtts)-1)*10) / 10
	return perdida, &j
}

func (m *Monitor) estadoLocked() EstadoGlobal {
	g := EstadoGlobal{Version: m.version, Actualizado: time.Now(), Inicio: m.inicio, IntervaloS: m.cfg.IntervaloS,
		IncidentesHoy: m.incHoy, UltimoIncidente: m.ultimoInc}
	for _, e := range m.equipos {
		c := *e
		c.hist, c.sonda = nil, nil
		switch e.Tipo {
		case "visor":
			g.Visores = append(g.Visores, c)
			if e.Estado == Online {
				g.VisoresOnline++
			}
		case "router":
			g.Router = c
		case "internet":
			g.Internet = c
		}
	}
	g.CentroActivo = time.Since(m.ultimoOnline) < 10*time.Minute
	return g
}

func (m *Monitor) escribirEstado(g EstadoGlobal) {
	j, _ := json.MarshalIndent(g, "", "  ")
	ruta := filepath.Join(m.dir, "estado.json")
	tmp := ruta + ".tmp"
	if os.WriteFile(tmp, j, 0o644) == nil {
		_ = os.Rename(tmp, ruta)
	}
}

// PuertaEnlace detecta el router por defecto del PC.
func PuertaEnlace() string {
	var txt string
	if ejec.EsWindows() {
		_, txt = ejec.PS(30*time.Second, "(Get-NetRoute -DestinationPrefix '0.0.0.0/0' | Sort RouteMetric | Select -First 1).NextHop")
	} else {
		_, txt = ejec.Run(10*time.Second, "sh", "-c", "ip route | awk '/default/{print $3; exit}'")
	}
	return ejec.Stdout(txt)
}
