package monitor

import (
	"encoding/json"
	"fmt"
	"os"
)

// Equipo es un visor u otro equipo vigilado.
type Equipo struct {
	ID    string `json:"id"`
	IP    string `json:"ip"`
	MAC   string `json:"mac,omitempty"`   // permite reencontrarlo si cambia la IP
	Serie string `json:"serie,omitempty"` // número de serie leído por USB
}

// Config se guarda en playbox.json junto al exe.
type Config struct {
	Visores           []Equipo `json:"visores"`
	Router            string   `json:"router"`   // vacío = puerta de enlace detectada
	Internet          string   `json:"internet"` // destino externo para separar LAN de Internet
	IntervaloS        int      `json:"intervalo_s"`
	EsperaMs          int      `json:"espera_ms"`
	FallosParaOffline int      `json:"fallos_para_offline"`
	LatenciaAltaMs    int      `json:"latencia_alta_ms"`
	RetencionDias     int      `json:"retencion_dias"`
	VentanaAntesMin   int      `json:"incidente_minutos_antes"`
	VentanaDespuesMin int      `json:"incidente_minutos_despues"`
}

// PorDefecto devuelve una configuración inicial con 10 visores sin IP.
func PorDefecto() Config {
	c := Config{Internet: "1.1.1.1"}
	for i := 1; i <= 10; i++ {
		c.Visores = append(c.Visores, Equipo{ID: fmt.Sprintf("PICO-%02d", i)})
	}
	c.completar()
	return c
}

func (c *Config) completar() {
	def := func(v *int, d int) {
		if *v <= 0 {
			*v = d
		}
	}
	def(&c.IntervaloS, 5)
	def(&c.EsperaMs, 1000)
	def(&c.FallosParaOffline, 3)
	def(&c.LatenciaAltaMs, 80)
	def(&c.RetencionDias, 30)
	def(&c.VentanaAntesMin, 5)
	def(&c.VentanaDespuesMin, 2)
}

// Cargar lee la configuración; si no existe, crea una plantilla y avisa.
func Cargar(ruta string) (Config, bool, error) {
	datos, err := os.ReadFile(ruta)
	if os.IsNotExist(err) {
		c := PorDefecto()
		j, _ := json.MarshalIndent(c, "", "  ")
		if err := os.WriteFile(ruta, j, 0o644); err != nil {
			return c, true, err
		}
		return c, true, nil
	}
	if err != nil {
		return Config{}, false, err
	}
	var c Config
	if err := json.Unmarshal(datos, &c); err != nil {
		return Config{}, false, fmt.Errorf("%s no es JSON válido: %w", ruta, err)
	}
	c.completar()
	return c, false, nil
}

// Guardar escribe la configuración en disco.
func Guardar(ruta string, c Config) error {
	j, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ruta, append(j, '\n'), 0o644)
}

// VisoresConIP cuenta cuántos visores tienen IP configurada.
func (c Config) VisoresConIP() int {
	n := 0
	for _, v := range c.Visores {
		if v.IP != "" {
			n++
		}
	}
	return n
}
