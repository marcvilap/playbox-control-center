// Package agentes recibe en la red local lo que envía el Agente PlayBox de
// cada gafa (el mismo protocolo que usaba la web: alta, latido, órdenes),
// sin Internet. El agente solo cambia su dirección base: http://IP-del-PC:47334/.
//
// Solo observa: no cambia el modo sesión ni el ahorro de energía de las gafas
// (modo prudente activado, sin ventanas de sesión).
package agentes

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Puerto en el que el PC escucha a los agentes (el de la voz de la gafa es 47333).
const Puerto = 47334

// Ruta base del protocolo heredado de la web.
const base = "/wp-json/playbox/v1/headsets/"

// FrescoTras: sin latido en este tiempo, los datos del agente se dan por viejos.
const FrescoTras = 150 * time.Second

// Estado es lo que el agente cuenta de la gafa (OBSERVADO en la propia gafa).
type Estado struct {
	BateriaPct   *int     `json:"bateria_pct,omitempty"`
	Cargando     bool     `json:"cargando"`
	TempC        *float64 `json:"temp_c,omitempty"`
	PantallaOn   *bool    `json:"pantalla_on,omitempty"`
	Volumen      *int     `json:"volumen,omitempty"`
	WifiSSID     string   `json:"wifi_ssid,omitempty"`
	WifiRSSI     *int     `json:"wifi_rssi,omitempty"`
	WifiMbps     *int     `json:"wifi_mbps,omitempty"`
	WifiIP       string   `json:"wifi_ip,omitempty"`
	App          string   `json:"app,omitempty"`
	AppNombre    string   `json:"app_nombre,omitempty"`
	MemLibreMB   *int     `json:"mem_libre_mb,omitempty"`
	MemTotalMB   *int     `json:"mem_total_mb,omitempty"`
	DiscoLibreMB *int     `json:"disco_libre_mb,omitempty"`
	Android      string   `json:"android,omitempty"`
	EncendidaMin *int     `json:"encendida_min,omitempty"`
	// Pilas de los mandos 1–5 (SDK Enterprise de PICO); nil = sin dato.
	MandoIzq *int `json:"mando_izq,omitempty"`
	MandoDer *int `json:"mando_der,omitempty"`
	// Estado de conexión de los mandos según el SDK (0 ninguno, 1 izq., 2 der., 3 ambos).
	MandosConectados *int `json:"mandos_conectados,omitempty"`
	SDKListo         bool `json:"sdk_listo"`
	Apagandose       bool `json:"apagandose,omitempty"`
}

// Gafa es una gafa dada de alta en este PC.
type Gafa struct {
	ID        int64     `json:"id"`
	UID       string    `json:"uid"`
	Serie     string    `json:"serie"`
	Modelo    string    `json:"modelo"`
	Nombre    string    `json:"nombre"`
	TokenHash string    `json:"token_hash"`
	Alta      time.Time `json:"alta"`
	Contacto  time.Time `json:"ultimo_contacto"`
	IP        string    `json:"ip"`
	Version   string    `json:"version"`
	Codigo    int       `json:"codigo"`
	Estado    Estado    `json:"estado"`
}

// Fresca indica si el último latido es reciente.
func (g Gafa) Fresca(ahora time.Time) bool { return ahora.Sub(g.Contacto) < FrescoTras }

// Orden es una orden en cola para una gafa (protocolo: id, kind, args).
type Orden struct {
	ID       int64          `json:"id"`
	Tipo     string         `json:"kind"`
	Args     map[string]any `json:"args"`
	Creada   time.Time      `json:"-"`
	Enviada  time.Time      `json:"-"`
	Resuelta bool           `json:"-"`
}

// Resultado de una orden, tal como lo devuelve el agente.
type Resultado struct {
	OrdenID int64  `json:"id"`
	Tipo    string `json:"tipo"`
	OK      bool   `json:"ok"`
	Mensaje string `json:"message"`
	Gafa    string `json:"gafa"`
	TS      time.Time
}

type registro struct {
	Clave     string  `json:"clave_alta"`
	Siguiente int64   `json:"siguiente_id"`
	Gafas     []*Gafa `json:"gafas"`
}

// Servidor atiende a los agentes en la red local.
type Servidor struct {
	dir string
	// OnEvento recibe lo que conviene guardar en la caja negra.
	OnEvento func(gafa, tipo, severidad, mensaje string, datos map[string]any)

	mu         sync.Mutex
	reg        registro
	cola       map[int64][]*Orden
	resultados []Resultado
	sigOrden   int64
	senal      string
	guardado   time.Time
	ln         net.Listener
	srv        *http.Server
}

// Nuevo carga (o crea) el registro de gafas en dir/agentes.
func Nuevo(dir string) (*Servidor, error) {
	s := &Servidor{dir: filepath.Join(dir, "agentes"), cola: map[int64][]*Orden{}, senal: nuevaSenal()}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return nil, err
	}
	reg, err := LeerRegistro(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	s.reg = reg
	if s.reg.Clave == "" {
		s.reg.Clave = aleatorio(24)
		s.reg.Siguiente = 1
		if err := s.guardarLocked(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func rutaRegistro(dir string) string { return filepath.Join(dir, "agentes", "registro.json") }

// LeerRegistro lee lo que guardó el proceso que escucha a los agentes
// (el servicio o la ventana): sirve al otro proceso para mostrar los datos.
func LeerRegistro(dir string) (registro, error) {
	var r registro
	b, err := os.ReadFile(rutaRegistro(dir))
	if err != nil {
		return r, err
	}
	err = json.Unmarshal(b, &r)
	if r.Siguiente < 1 {
		r.Siguiente = 1
	}
	return r, err
}

// GafasGuardadas devuelve las gafas del registro en disco (sin tokens).
func GafasGuardadas(dir string) []Gafa {
	r, err := LeerRegistro(dir)
	if err != nil {
		return nil
	}
	return copiar(r.Gafas)
}

// ClaveAlta es la clave que el agente necesita para darse de alta en este PC.
func (s *Servidor) ClaveAlta() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reg.Clave
}

// Gafas devuelve una copia de las gafas dadas de alta (sin tokens).
func (s *Servidor) Gafas() []Gafa {
	s.mu.Lock()
	defer s.mu.Unlock()
	return copiar(s.reg.Gafas)
}

func copiar(gs []*Gafa) []Gafa {
	out := make([]Gafa, 0, len(gs))
	for _, g := range gs {
		c := *g
		c.TokenHash = ""
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Nombre < out[j].Nombre })
	return out
}

// Escuchar abre el puerto en todas las interfaces. Devuelve error si ya lo
// usa otro proceso (p. ej. el servicio): entonces este proceso solo lee el registro.
func (s *Servidor) Escuchar(puerto int) error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", puerto))
	if err != nil {
		return err
	}
	s.ln = ln
	s.srv = &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second}
	go func() { _ = s.srv.Serve(ln) }()
	return nil
}

// Cerrar deja de escuchar y guarda el registro.
func (s *Servidor) Cerrar() {
	if s.srv == nil {
		return
	}
	_ = s.srv.Close()
	s.mu.Lock()
	_ = s.guardarLocked()
	s.mu.Unlock()
}

// Handler sirve el protocolo de los agentes.
func (s *Servidor) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(base+"enroll", s.alta)
	mux.HandleFunc(base+"heartbeat", s.latido)
	mux.HandleFunc(base+"commands", s.consulta)
	mux.HandleFunc(base+"apk", func(w http.ResponseWriter, _ *http.Request) {
		errorJSON(w, http.StatusNotFound, "playbox_headsets_none", "No hay versión publicada.")
	})
	mux.HandleFunc(base+"voice", func(w http.ResponseWriter, _ *http.Request) {
		errorJSON(w, http.StatusNotFound, "playbox_headsets_none", "Ese mensaje ya no existe.")
	})
	mux.HandleFunc("/playbox-senal/", func(w http.ResponseWriter, _ *http.Request) {
		s.mu.Lock()
		v := s.senal
		s.mu.Unlock()
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(v))
	})
	return limitar(mux)
}

func limitar(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
		h.ServeHTTP(w, r)
	})
}

var reUID = regexp.MustCompile(`[^A-Za-z0-9._:-]`)

func (s *Servidor) alta(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		errorJSON(w, http.StatusMethodNotAllowed, "metodo", "Solo POST.")
		return
	}
	var p struct{ Key, UID, Serial, Model, Name string }
	if json.NewDecoder(r.Body).Decode(&p) != nil {
		errorJSON(w, http.StatusBadRequest, "json", "Petición incorrecta.")
		return
	}
	clave := strings.SplitN(p.Key, "@", 2)[0]
	s.mu.Lock()
	defer s.mu.Unlock()
	if subtle.ConstantTimeCompare([]byte(clave), []byte(s.reg.Clave)) != 1 {
		errorJSON(w, http.StatusForbidden, "playbox_headsets_key", "Clave de alta incorrecta.")
		s.evento("", "agente_clave_mala", "aviso", "Un agente probó una clave de alta incorrecta", map[string]any{"ip": ipRemota(r)})
		return
	}
	uid := cortar(reUID.ReplaceAllString(p.UID, ""), 64)
	if len(uid) < 6 {
		errorJSON(w, http.StatusBadRequest, "playbox_headsets_uid", "Falta el identificador de la gafa.")
		return
	}
	token := aleatorio(40)
	var g *Gafa
	for _, x := range s.reg.Gafas {
		if x.UID == uid {
			g = x
		}
	}
	if g == nil {
		g = &Gafa{ID: s.reg.Siguiente, UID: uid, Alta: time.Now()}
		s.reg.Siguiente++
		s.reg.Gafas = append(s.reg.Gafas, g)
	}
	g.Serie, g.Modelo = cortar(p.Serial, 64), cortar(p.Model, 60)
	if n := cortar(strings.TrimSpace(p.Name), 60); n != "" {
		g.Nombre = n
	} else if g.Nombre == "" {
		g.Nombre = fmt.Sprintf("Gafa %d", g.ID)
	}
	g.TokenHash = huella(token)
	g.IP = ipRemota(r)
	g.Contacto = time.Now()
	_ = s.guardarLocked()
	s.evento(g.Nombre, "agente_alta", "info", "El agente de "+g.Nombre+" se ha dado de alta en este PC",
		map[string]any{"serie": g.Serie, "modelo": g.Modelo, "ip": g.IP})
	escribirJSON(w, map[string]any{"id": g.ID, "token": token})
}

var reAuth = regexp.MustCompile(`^(\d+)\.([A-Za-z0-9]{20,64})$`)

// autenticar devuelve la gafa de la cabecera X-PB-Agent («id.token»). Con s.mu tomado.
func (s *Servidor) autenticar(r *http.Request) *Gafa {
	m := reAuth.FindStringSubmatch(r.Header.Get("X-PB-Agent"))
	if m == nil {
		return nil
	}
	h := huella(m[2])
	for _, g := range s.reg.Gafas {
		if fmt.Sprint(g.ID) == m[1] && subtle.ConstantTimeCompare([]byte(g.TokenHash), []byte(h)) == 1 {
			return g
		}
	}
	return nil
}

type latidoIn struct {
	Code    int              `json:"code"`
	Version string           `json:"version"`
	State   map[string]any   `json:"state"`
	Events  []map[string]any `json:"events"`
	Results []map[string]any `json:"results"`
}

func (s *Servidor) latido(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		errorJSON(w, http.StatusMethodNotAllowed, "metodo", "Solo POST.")
		return
	}
	var p latidoIn
	if json.NewDecoder(r.Body).Decode(&p) != nil {
		errorJSON(w, http.StatusBadRequest, "json", "Petición incorrecta.")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.autenticar(r)
	if g == nil {
		// 401: la gafa vuelve a darse de alta con la clave que guarda.
		errorJSON(w, http.StatusUnauthorized, "playbox_headsets_auth", "Gafa no reconocida.")
		return
	}
	ahora := time.Now()
	antes := g.Estado
	g.Estado = Limpiar(p.State)
	g.Codigo, g.Version = p.Code, cortar(p.Version, 20)
	g.IP, g.Contacto = ipRemota(r), ahora
	s.avisosEstado(g, antes)
	for _, e := range p.Events {
		s.eventoAgente(g, e)
	}
	s.guardarResultados(g, p.Results)
	_ = s.guardarLocked() // el otro proceso (ventana o servicio) lo lee de aquí
	escribirJSON(w, map[string]any{
		"ok":       true,
		"saved":    len(p.Events),
		"interval": 30,
		"poll":     0,
		"update":   nil,
		"name":     g.Nombre,
		"sessions": []any{}, // las reservas se quedan en la web: aquí no hay ventanas de sesión
		"commands": s.tomarOrdenes(g.ID, ahora),
		"now":      ahora.UnixMilli(),
		"settings": map[string]any{
			"safe":        true, // modo prudente: el PC solo observa
			"picoSession": false,
			"autoSession": false,
			"wakeable":    false,
			"awakeUntil":  0,
			"alwaysOn":    false,
			"keepApps":    []string{},
			"talkKey":     "",
			"signal":      "http://" + hostLocal(r) + "/playbox-senal/centro.txt",
		},
	})
}

func (s *Servidor) consulta(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.autenticar(r)
	if g == nil {
		errorJSON(w, http.StatusUnauthorized, "playbox_headsets_auth", "Gafa no reconocida.")
		return
	}
	ahora := time.Now()
	g.Contacto, g.IP = ahora, ipRemota(r)
	escribirJSON(w, map[string]any{"commands": s.tomarOrdenes(g.ID, ahora), "poll": 0, "now": ahora.UnixMilli(), "alwaysOn": false})
}

// TTL de una orden sin recoger (como en la web).
const ttlOrden = 5 * time.Minute

// Encolar deja una orden para una gafa y avisa por la señal.
func (s *Servidor) Encolar(id int64, tipo string, args map[string]any) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existe := false
	for _, g := range s.reg.Gafas {
		existe = existe || g.ID == id
	}
	if !existe {
		return 0, fmt.Errorf("gafa %d no dada de alta", id)
	}
	if args == nil {
		args = map[string]any{}
	}
	// La misma orden aún pendiente no se repite (dos clics seguidos).
	var cola []*Orden
	for _, o := range s.cola[id] {
		if o.Tipo != tipo || !o.Enviada.IsZero() {
			cola = append(cola, o)
		}
	}
	s.sigOrden++
	cola = append(cola, &Orden{ID: s.sigOrden, Tipo: tipo, Args: args, Creada: time.Now()})
	s.cola[id] = cola
	s.senal = nuevaSenal()
	return s.sigOrden, nil
}

func (s *Servidor) tomarOrdenes(id int64, ahora time.Time) []*Orden {
	out := []*Orden{}
	var quedan []*Orden
	for _, o := range s.cola[id] {
		if ahora.Sub(o.Creada) > ttlOrden || o.Resuelta {
			continue
		}
		quedan = append(quedan, o)
		// Sin resultado a los 15 s: se vuelve a mandar (el agente no repite una orden hecha).
		if o.Enviada.IsZero() || ahora.Sub(o.Enviada) > 15*time.Second {
			o.Enviada = ahora
			out = append(out, o)
		}
	}
	s.cola[id] = quedan
	return out
}

func (s *Servidor) guardarResultados(g *Gafa, rs []map[string]any) {
	for _, r := range rs {
		id := int64(num(r["id"]))
		for _, o := range s.cola[g.ID] {
			if o.ID != id {
				continue
			}
			o.Resuelta = true
			ok, _ := r["ok"].(bool)
			msg := cortar(fmt.Sprint(valor(r["message"])), 200)
			s.resultados = append(s.resultados, Resultado{OrdenID: id, Tipo: o.Tipo, OK: ok, Mensaje: msg, Gafa: g.Nombre, TS: time.Now()})
			if len(s.resultados) > 50 {
				s.resultados = s.resultados[len(s.resultados)-50:]
			}
			sev := "info"
			if !ok {
				sev = "aviso"
			}
			s.evento(g.Nombre, "orden_resultado", sev, fmt.Sprintf("%s: orden «%s» %s", g.Nombre, o.Tipo, map[bool]string{true: "hecha", false: "falló"}[ok]),
				map[string]any{"orden": o.Tipo, "ok": ok, "mensaje": msg})
		}
	}
}

// Resultados devuelve los últimos resultados de órdenes.
func (s *Servidor) Resultados() []Resultado {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Resultado(nil), s.resultados...)
}

// avisosEstado registra cambios que importan: mandos con pilas bajas, apagado.
func (s *Servidor) avisosEstado(g *Gafa, antes Estado) {
	e := g.Estado
	for _, m := range []struct {
		lado        string
		ahora, prev *int
	}{{"izquierdo", e.MandoIzq, antes.MandoIzq}, {"derecho", e.MandoDer, antes.MandoDer}} {
		if m.ahora != nil && *m.ahora <= 1 && (m.prev == nil || *m.prev > 1) {
			s.evento(g.Nombre, "mando_pilas_bajas", "aviso", fmt.Sprintf("%s: mando %s con pilas bajas (%d/5)", g.Nombre, m.lado, *m.ahora),
				map[string]any{"lado": m.lado, "nivel": *m.ahora})
		}
	}
	if e.Apagandose && !antes.Apagandose {
		s.evento(g.Nombre, "gafa_apagandose", "info", g.Nombre+" se está apagando", nil)
	}
}

var reTipo = regexp.MustCompile(`[^a-z0-9_]`)

func (s *Servidor) eventoAgente(g *Gafa, e map[string]any) {
	tipo := cortar(reTipo.ReplaceAllString(strings.ToLower(fmt.Sprint(valor(e["kind"]))), ""), 32)
	if tipo == "" {
		return
	}
	datos, _ := e["data"].(map[string]any)
	if datos == nil {
		datos = map[string]any{}
	}
	if at := int64(num(e["at"])); at > 1600000000000 && at < time.Now().Add(10*time.Minute).UnixMilli() {
		datos["hora_gafa"] = time.UnixMilli(at).UTC().Format(time.RFC3339)
	}
	s.evento(g.Nombre, "agente_"+tipo, "info", g.Nombre+": "+strings.ReplaceAll(tipo, "_", " "), datos)
}

func (s *Servidor) evento(gafa, tipo, sev, msg string, datos map[string]any) {
	if s.OnEvento != nil {
		s.OnEvento(gafa, tipo, sev, msg, datos)
	}
}

func (s *Servidor) guardarLocked() error {
	s.guardado = time.Now()
	b, err := json.MarshalIndent(s.reg, "", "  ")
	if err != nil {
		return err
	}
	ruta := filepath.Join(s.dir, "registro.json")
	tmp := ruta + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, ruta)
}

// Limpiar se queda con lo que se espera del estado y en su tipo (como la web).
func Limpiar(st map[string]any) Estado {
	grupo := func(k string) map[string]any {
		m, _ := st[k].(map[string]any)
		if m == nil {
			m = map[string]any{}
		}
		return m
	}
	bat, scr, wifi, app := grupo("battery"), grupo("screen"), grupo("wifi"), grupo("app")
	mem, sto, sys, pico := grupo("memory"), grupo("storage"), grupo("system"), grupo("pico")
	var e Estado
	e.BateriaPct = entero(bat["level"])
	e.Cargando, _ = bat["charging"].(bool)
	if t, ok := bat["temp"].(float64); ok {
		e.TempC = &t
	}
	if v, ok := scr["on"].(bool); ok {
		e.PantallaOn = &v
	}
	e.Volumen = entero(scr["volume"])
	e.WifiSSID = cortar(texto(wifi["ssid"]), 64)
	e.WifiRSSI, e.WifiMbps = entero(wifi["rssi"]), entero(wifi["mbps"])
	if ip := net.ParseIP(texto(wifi["ip"])); ip != nil && ip.To4() != nil && ip.IsPrivate() {
		e.WifiIP = ip.String()
	}
	e.App, e.AppNombre = cortar(texto(app["pkg"]), 120), cortar(texto(app["label"]), 80)
	e.MemLibreMB, e.MemTotalMB, e.DiscoLibreMB = entero(mem["availMb"]), entero(mem["totalMb"]), entero(sto["freeMb"])
	e.Android, e.EncendidaMin = cortar(texto(sys["android"]), 20), entero(sys["uptimeMin"])
	e.SDKListo, _ = pico["ready"].(bool)
	e.MandoIzq, e.MandoDer = nivelMando(pico["ctrlLeft"]), nivelMando(pico["ctrlRight"])
	e.MandosConectados = entero(pico["ctrlConnected"])
	e.Apagandose, _ = st["off"].(bool)
	return e
}

// nivelMando: el SDK da 1–5; 0 o fuera de rango = sin dato.
func nivelMando(v any) *int {
	n := entero(v)
	if n == nil || *n < 1 || *n > 5 {
		return nil
	}
	return n
}

func entero(v any) *int {
	f, ok := v.(float64)
	if !ok {
		return nil
	}
	n := int(f)
	return &n
}

func num(v any) float64 {
	f, _ := v.(float64)
	return f
}

func texto(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func valor(v any) any {
	if v == nil {
		return ""
	}
	return v
}

func cortar(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

func huella(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

const letras = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789"

func aleatorio(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = letras[int(b[i])%len(letras)]
	}
	return string(b)
}

func nuevaSenal() string { return aleatorio(12) }

func ipRemota(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

// hostLocal es «IP:puerto» del PC tal como lo ve la gafa.
func hostLocal(r *http.Request) string {
	if a, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
		return a.String()
	}
	return r.Host
}

func escribirJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func errorJSON(w http.ResponseWriter, estado int, codigo, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(estado)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": codigo, "message": msg, "data": map[string]int{"status": estado}})
}

// IPLocalPara elige la IP de este PC en la misma red que la gafa.
func IPLocalPara(ipGafa string) string {
	objetivo := net.ParseIP(ipGafa)
	addrs, _ := net.InterfaceAddrs()
	var primera string
	for _, a := range addrs {
		n, ok := a.(*net.IPNet)
		if !ok || n.IP.To4() == nil || n.IP.IsLoopback() || n.IP.IsLinkLocalUnicast() {
			continue
		}
		if objetivo != nil && n.Contains(objetivo) {
			return n.IP.String()
		}
		if primera == "" && n.IP.IsPrivate() {
			primera = n.IP.String()
		}
	}
	return primera
}
