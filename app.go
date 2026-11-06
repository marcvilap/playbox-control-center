package main

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"playbox/internal/actualizar"
	"playbox/internal/descubrir"
	"playbox/internal/ejec"
	"playbox/internal/monitor"
	"playbox/internal/red"
	"playbox/internal/sonda"
)

//go:embed ui/index.html
var paginaUI []byte

// App es el controlador de la ventana: arranca/para la vigilancia y
// atiende los botones de la interfaz.
type App struct {
	token string
	salir func() // cierra la ventana

	mu        sync.Mutex
	mon       *monitor.Monitor
	cancelar  context.CancelFunc
	terminado chan struct{}
	simulando bool
	dirMon    string

	upd struct {
		comprobando bool
		instalando  bool
		nueva       *actualizar.Manifiesto
		err         string
		hora        time.Time
	}
	busqueda struct {
		enCurso   bool
		resultado *red.Resultado
		err       string
	}
	f0 struct {
		enCurso bool
		fase    string
		carpeta string
		err     string
	}
}

func nuevaApp() *App {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return &App{token: hex.EncodeToString(b), salir: func() {}}
}

// arrancarMonitor para la vigilancia en curso y lanza una nueva.
func (a *App) arrancarMonitor(simular bool) error {
	a.pararMonitor()
	var cfg monitor.Config
	dir := dirDatos()
	if simular {
		cfg, dir = configSimulacion(), filepath.Join(dirExe(), "datos-simulacion")
	} else {
		var err error
		if cfg, _, err = monitor.Cargar(rutaConfig()); err != nil {
			return err
		}
	}
	mon, err := monitor.Nuevo(cfg, dir, Version, nil)
	if err != nil {
		return err
	}
	if simular {
		mon.NuevaSonda = sonda.NuevaSimulada(time.Now())
	}
	ctx, cancel := context.WithCancel(context.Background())
	hecho := make(chan struct{})
	go func() { _ = mon.Ejecutar(ctx); close(hecho) }()
	a.mu.Lock()
	a.mon, a.cancelar, a.terminado, a.simulando, a.dirMon = mon, cancel, hecho, simular, dir
	a.mu.Unlock()
	return nil
}

func (a *App) pararMonitor() {
	a.mu.Lock()
	cancel, hecho := a.cancelar, a.terminado
	a.mon, a.cancelar = nil, nil
	a.mu.Unlock()
	if cancel != nil {
		cancel()
		<-hecho
	}
}

func (a *App) comprobarActualizacion() {
	a.mu.Lock()
	if a.upd.comprobando {
		a.mu.Unlock()
		return
	}
	a.upd.comprobando = true
	a.mu.Unlock()
	m, nueva, err := actualizar.Comprobar(configActualizacion())
	a.mu.Lock()
	defer a.mu.Unlock()
	a.upd.comprobando, a.upd.hora, a.upd.err, a.upd.nueva = false, time.Now(), "", nil
	if err != nil {
		a.upd.err = "No se pudo comprobar (¿sin Internet?)"
	} else if nueva {
		a.upd.nueva = m
	}
}

// ---------------------------------------------------------------- servidor

func (a *App) servidor() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" || r.URL.Query().Get("t") != a.token {
			http.Error(w, "no autorizado", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(paginaUI)
	})
	api := func(ruta string, f func(r *http.Request) (any, error)) {
		mux.HandleFunc("/api/"+ruta, func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Token") != a.token {
				http.Error(w, "no autorizado", http.StatusForbidden)
				return
			}
			if ruta != "estado" && r.Method != http.MethodPost {
				http.Error(w, "usa POST", http.StatusMethodNotAllowed)
				return
			}
			res, err := f(r)
			w.Header().Set("Content-Type", "application/json")
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			_ = json.NewEncoder(w).Encode(res)
		})
	}
	api("estado", a.apiEstado)
	api("actualizar/comprobar", func(*http.Request) (any, error) { go a.comprobarActualizacion(); return "ok", nil })
	api("actualizar/instalar", a.apiInstalarActualizacion)
	api("config", a.apiConfig)
	api("simulacion", a.apiSimulacion)
	api("f0", a.apiF0)
	api("abrir", a.apiAbrir)
	api("instalar", a.apiInstalarApp)
	api("buscar", a.apiBuscar)
	api("salir", func(*http.Request) (any, error) {
		go func() { time.Sleep(200 * time.Millisecond); a.salir() }()
		return "ok", nil
	})
	go func() { _ = http.Serve(ln, mux) }()
	return fmt.Sprintf("http://%s/?t=%s", ln.Addr().String(), a.token), nil
}

type incidenteBreve struct {
	ID          string    `json:"id"`
	Tipo        string    `json:"tipo"`
	Severidad   string    `json:"severidad"`
	Dispositivo string    `json:"dispositivo"`
	Resumen     string    `json:"resumen"`
	Inicio      time.Time `json:"inicio"`
	Recuperado  bool      `json:"recuperado"`
}

func leerIncidentes(dir string, n int) []incidenteBreve {
	rutas, _ := filepath.Glob(filepath.Join(dir, "incidentes", "*.json"))
	sort.Sort(sort.Reverse(sort.StringSlice(rutas))) // el nombre empieza por la fecha
	var out []incidenteBreve
	for _, r := range rutas {
		if len(out) == n {
			break
		}
		datos, err := os.ReadFile(r)
		if err != nil {
			continue
		}
		var e struct {
			incidenteBreve
			Recuperado bool `json:"recuperado_al_cerrar"`
		}
		if json.Unmarshal(datos, &e) == nil {
			e.incidenteBreve.Recuperado = e.Recuperado
			out = append(out, e.incidenteBreve)
		}
	}
	return out
}

// incidentesRecientes une los incidentes aún abiertos (en memoria, su
// expediente se escribe 2 min después) con los ya guardados en disco.
func incidentesRecientes(eventos []monitor.Evento, dir string, n int) []incidenteBreve {
	vistos := map[string]bool{}
	out := leerIncidentes(dir, n) // los guardados primero: traen «recuperado»
	for _, inc := range out {
		vistos[inc.ID] = true
	}
	for i := len(eventos) - 1; i >= 0; i-- {
		e := eventos[i]
		if e.Incidente == "" || vistos[e.Incidente] {
			continue
		}
		vistos[e.Incidente] = true
		out = append(out, incidenteBreve{ID: e.Incidente, Tipo: e.Tipo, Severidad: e.Severidad,
			Dispositivo: e.Dispositivo, Resumen: e.Mensaje, Inicio: e.TS})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Inicio.After(out[j].Inicio) })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func (a *App) apiEstado(*http.Request) (any, error) {
	a.mu.Lock()
	mon, sim, dir := a.mon, a.simulando, a.dirMon
	upd := map[string]any{"comprobando": a.upd.comprobando, "instalando": a.upd.instalando, "error": a.upd.err}
	if a.upd.nueva != nil {
		upd["nueva"] = a.upd.nueva.Version
		upd["notas"] = a.upd.nueva.Notas
	}
	if !a.upd.hora.IsZero() {
		upd["hora"] = a.upd.hora.Format("15:04")
	}
	f0 := map[string]any{"en_curso": a.f0.enCurso, "fase": a.f0.fase, "carpeta": a.f0.carpeta, "error": a.f0.err}
	busq := map[string]any{"en_curso": a.busqueda.enCurso, "resultado": a.busqueda.resultado, "error": a.busqueda.err}
	a.mu.Unlock()

	cfg, _, _ := monitor.Cargar(rutaConfig())
	res := map[string]any{
		"version": Version, "simulando": sim, "actualizacion": upd, "f0": f0, "busqueda": busq,
		"instalado": estaInstalado(), "carpeta": dirExe(), "visores_config": cfg.Visores,
	}
	if mon != nil {
		res["estado"] = mon.Instantanea()
		recientes := mon.Caja().Recientes(200)
		ev := recientes
		if len(ev) > 9 {
			ev = ev[len(ev)-9:]
		}
		res["eventos"] = ev
		res["incidentes"] = incidentesRecientes(recientes, dir, 8)
	}
	return res, nil
}

func leerJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 64<<10)).Decode(v)
}

func (a *App) apiInstalarActualizacion(*http.Request) (any, error) {
	a.mu.Lock()
	m := a.upd.nueva
	if m == nil || a.upd.instalando {
		a.mu.Unlock()
		return nil, fmt.Errorf("no hay ninguna versión nueva pendiente")
	}
	a.upd.instalando = true
	a.mu.Unlock()
	ruta, err := actualizar.Instalar(configActualizacion(), m)
	if err != nil {
		a.mu.Lock()
		a.upd.instalando, a.upd.err = false, err.Error()
		a.mu.Unlock()
		return nil, err
	}
	// Reabrir con la versión nueva y cerrar esta ventana.
	go func() {
		time.Sleep(300 * time.Millisecond)
		a.pararMonitor()
		liberarInstanciaUnica()
		cmd := exec.Command(ruta)
		cmd.Env = append(os.Environ(), envRecienActualizado+"=1")
		_ = cmd.Start()
		a.salir()
	}()
	return map[string]string{"version": m.Version}, nil
}

func (a *App) apiConfig(r *http.Request) (any, error) {
	var in struct {
		Visores []monitor.Equipo `json:"visores"`
	}
	if err := leerJSON(r, &in); err != nil {
		return nil, err
	}
	cfg, _, err := monitor.Cargar(rutaConfig())
	if err != nil {
		return nil, err
	}
	for _, v := range in.Visores {
		ip := strings.TrimSpace(v.IP)
		if ip != "" && net.ParseIP(ip).To4() == nil {
			return nil, fmt.Errorf("%s: «%s» no es una IP válida", v.ID, ip)
		}
		for i := range cfg.Visores {
			if cfg.Visores[i].ID == v.ID {
				cfg.Visores[i].IP = ip
			}
		}
	}
	if err := monitor.Guardar(rutaConfig(), cfg); err != nil {
		return nil, err
	}
	a.mu.Lock()
	sim := a.simulando
	a.mu.Unlock()
	if !sim {
		if err := a.arrancarMonitor(false); err != nil {
			return nil, err
		}
	}
	return map[string]int{"visores_con_ip": cfg.VisoresConIP()}, nil
}

func (a *App) apiSimulacion(r *http.Request) (any, error) {
	var in struct {
		Activar bool `json:"activar"`
	}
	if err := leerJSON(r, &in); err != nil {
		return nil, err
	}
	return "ok", a.arrancarMonitor(in.Activar)
}

func (a *App) apiF0(r *http.Request) (any, error) {
	var in struct {
		Fase string `json:"fase"`
	}
	if err := leerJSON(r, &in); err != nil || in.Fase == "" {
		return nil, fmt.Errorf("falta la fase")
	}
	a.mu.Lock()
	if a.f0.enCurso {
		a.mu.Unlock()
		return nil, fmt.Errorf("ya hay un descubrimiento en curso")
	}
	a.f0.enCurso, a.f0.fase, a.f0.err, a.f0.carpeta = true, in.Fase, "", ""
	a.mu.Unlock()
	go func() {
		res, err := descubrir.Ejecutar(descubrir.Opciones{Fase: in.Fase, DirBase: dirInformes(), PingN: 60, Version: Version})
		a.mu.Lock()
		defer a.mu.Unlock()
		a.f0.enCurso = false
		if err != nil {
			a.f0.err = err.Error()
			return
		}
		a.f0.carpeta = res.Carpeta
		abrirCarpeta(res.Carpeta)
	}()
	return "ok", nil
}

// apiBuscar rastrea la red local para encontrar los visores (solo lectura).
func (a *App) apiBuscar(*http.Request) (any, error) {
	a.mu.Lock()
	if a.busqueda.enCurso {
		a.mu.Unlock()
		return "ok", nil
	}
	a.busqueda.enCurso, a.busqueda.err = true, ""
	router := ""
	if a.mon != nil {
		router = a.mon.Instantanea().Router.IP
	}
	a.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		res, err := red.Escanear(ctx, sonda.Nueva, router)
		a.mu.Lock()
		defer a.mu.Unlock()
		a.busqueda.enCurso = false
		if err != nil {
			a.busqueda.err = err.Error()
			return
		}
		a.busqueda.resultado = res
	}()
	return "ok", nil
}

func (a *App) apiAbrir(r *http.Request) (any, error) {
	var in struct {
		Que string `json:"que"`
	}
	if err := leerJSON(r, &in); err != nil {
		return nil, err
	}
	a.mu.Lock()
	dir := a.dirMon
	a.mu.Unlock()
	rutas := map[string]string{"datos": dir, "incidentes": filepath.Join(dir, "incidentes"), "informes": dirInformes()}
	ruta, ok := rutas[in.Que]
	if !ok {
		return nil, fmt.Errorf("carpeta desconocida")
	}
	_ = os.MkdirAll(ruta, 0o755)
	abrirCarpeta(ruta)
	return "ok", nil
}

func (a *App) apiInstalarApp(*http.Request) (any, error) {
	ruta, err := instalarApp()
	if err != nil {
		return nil, err
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		a.pararMonitor()
		liberarInstanciaUnica()
		_ = exec.Command(ruta).Start()
		a.salir()
	}()
	return map[string]string{"ruta": ruta}, nil
}

func abrirCarpeta(ruta string) {
	if ejec.EsWindows() {
		_ = exec.Command("explorer.exe", ruta).Start()
	}
}

// iniciarApp arranca vigilancia, servidor y comprobación de versiones.
func iniciarApp() (*App, string, error) {
	actualizar.Limpiar()
	a := nuevaApp()
	if err := a.arrancarMonitor(false); err != nil {
		return nil, "", err
	}
	url, err := a.servidor()
	if err != nil {
		return nil, "", err
	}
	go func() {
		a.comprobarActualizacion()
		for range time.Tick(6 * time.Hour) {
			a.comprobarActualizacion()
		}
	}()
	return a, url, nil
}
