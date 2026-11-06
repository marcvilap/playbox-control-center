package monitor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Evento normalizado de la caja negra (spec §8).
type Evento struct {
	TS          time.Time      `json:"ts"` // UTC
	Fuente      string         `json:"fuente"`
	Dispositivo string         `json:"dispositivo,omitempty"`
	Tipo        string         `json:"tipo"`
	Severidad   string         `json:"severidad"` // info, aviso, alta, critica
	Mensaje     string         `json:"mensaje"`
	Fiabilidad  string         `json:"fiabilidad"` // observado, inferido
	Incidente   string         `json:"incidente,omitempty"`
	Datos       map[string]any `json:"datos,omitempty"`
}

// Muestra es una medición puntual de un equipo.
type Muestra struct {
	TS    time.Time `json:"ts"`
	Disp  string    `json:"disp"`
	OK    bool      `json:"ok"`
	RTTms float64   `json:"rtt_ms,omitempty"`
}

// Caja guarda eventos en disco (un JSONL por día) y mantiene en memoria
// las últimas muestras y eventos para los expedientes de incidente.
type Caja struct {
	mu        sync.Mutex
	dir       string
	retencion time.Duration
	memoria   time.Duration
	muestras  []Muestra
	eventos   []Evento
	archivo   *os.File
	dia       string
}

// NuevaCaja abre la caja negra en dir.
func NuevaCaja(dir string, retencionDias int, memoria time.Duration) (*Caja, error) {
	if err := os.MkdirAll(filepath.Join(dir, "eventos"), 0o755); err != nil {
		return nil, err
	}
	_ = os.MkdirAll(filepath.Join(dir, "incidentes"), 0o755)
	c := &Caja{dir: dir, retencion: time.Duration(retencionDias) * 24 * time.Hour, memoria: memoria}
	c.Purgar()
	return c, nil
}

// Registrar guarda un evento (siempre persiste, aunque el fallo se resuelva solo).
func (c *Caja) Registrar(e Evento) {
	if e.TS.IsZero() {
		e.TS = time.Now().UTC()
	}
	e.TS = e.TS.UTC()
	if e.Fiabilidad == "" {
		e.Fiabilidad = "observado"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.eventos = append(c.eventos, e)
	c.recortar()
	dia := e.TS.Format("2006-01-02")
	if c.archivo == nil || dia != c.dia {
		if c.archivo != nil {
			c.archivo.Close()
		}
		f, err := os.OpenFile(filepath.Join(c.dir, "eventos", "eventos-"+dia+".jsonl"),
			os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			c.archivo = nil
			return
		}
		c.archivo, c.dia = f, dia
	}
	j, _ := json.Marshal(e)
	_, _ = c.archivo.Write(append(j, '\n'))
	_ = c.archivo.Sync()
}

// Anotar añade una muestra al buffer circular en memoria.
func (c *Caja) Anotar(m Muestra) {
	c.mu.Lock()
	c.muestras = append(c.muestras, m)
	c.recortar()
	c.mu.Unlock()
}

func (c *Caja) recortar() {
	limite := time.Now().Add(-c.memoria)
	i := 0
	for i < len(c.muestras) && c.muestras[i].TS.Before(limite) {
		i++
	}
	c.muestras = c.muestras[i:]
	j := 0
	for j < len(c.eventos) && c.eventos[j].TS.Before(limite) {
		j++
	}
	c.eventos = c.eventos[j:]
}

// Ventana devuelve copias de las muestras y eventos entre desde y hasta.
func (c *Caja) Ventana(desde, hasta time.Time) ([]Muestra, []Evento) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var ms []Muestra
	for _, m := range c.muestras {
		if !m.TS.Before(desde) && !m.TS.After(hasta) {
			ms = append(ms, m)
		}
	}
	var es []Evento
	for _, e := range c.eventos {
		if !e.TS.Before(desde) && !e.TS.After(hasta) {
			es = append(es, e)
		}
	}
	return ms, es
}

// Recientes devuelve los últimos n eventos en memoria (el más nuevo al final).
func (c *Caja) Recientes(n int) []Evento {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.eventos) < n {
		n = len(c.eventos)
	}
	return append([]Evento(nil), c.eventos[len(c.eventos)-n:]...)
}

// GuardarExpediente escribe un expediente de incidente en incidentes/<id>.json.
func (c *Caja) GuardarExpediente(id string, exp any) (string, error) {
	ruta := filepath.Join(c.dir, "incidentes", id+".json")
	j, err := json.MarshalIndent(exp, "", "  ")
	if err != nil {
		return "", err
	}
	return ruta, os.WriteFile(ruta, j, 0o644)
}

// Purgar borra eventos e incidentes más antiguos que la retención.
func (c *Caja) Purgar() {
	limite := time.Now().Add(-c.retencion)
	for _, sub := range []string{"eventos", "incidentes"} {
		entradas, _ := os.ReadDir(filepath.Join(c.dir, sub))
		for _, e := range entradas {
			if info, err := e.Info(); err == nil && info.ModTime().Before(limite) &&
				(strings.HasSuffix(e.Name(), ".jsonl") || strings.HasSuffix(e.Name(), ".json")) {
				_ = os.Remove(filepath.Join(c.dir, sub, e.Name()))
			}
		}
	}
}

// Cerrar cierra el archivo del día.
func (c *Caja) Cerrar() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.archivo != nil {
		c.archivo.Close()
		c.archivo = nil
	}
}
