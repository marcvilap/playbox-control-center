// Package pc mide la salud del PC de juego sin cargarlo: CPU, RAM y disco
// con llamadas nativas; GPU con nvidia-smi si existe; teclado y ratón por PnP.
package pc

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"playbox/internal/ejec"
)

// GPU de la tarjeta gráfica principal (solo NVIDIA por ahora).
type GPU struct {
	Nombre    string  `json:"nombre"`
	UsoPct    float64 `json:"uso_pct"`
	TempC     float64 `json:"temp_c"`
	VRAMUsoMB float64 `json:"vram_usada_mb"`
	VRAMTotMB float64 `json:"vram_total_mb"`
}

// Entrada resume teclados y ratones conectados.
type Entrada struct {
	Teclados  int       `json:"teclados"`
	Ratones   int       `json:"ratones"`
	Problemas []string  `json:"problemas"`
	Leido     time.Time `json:"leido"`
}

// Salud del PC en un momento dado.
type Salud struct {
	CPUPct       float64   `json:"cpu_pct"`
	RAMPct       float64   `json:"ram_pct"`
	RAMTotalGB   float64   `json:"ram_total_gb"`
	DiscoLibreGB float64   `json:"disco_libre_gb"`
	DiscoPct     float64   `json:"disco_usado_pct"`
	GPU          *GPU      `json:"gpu,omitempty"`
	Entrada      *Entrada  `json:"entrada,omitempty"`
	Leido        time.Time `json:"leido"`
}

// LeerGPU consulta nvidia-smi (si no hay NVIDIA, devuelve nil).
func LeerGPU() *GPU {
	code, out := ejec.Run(8*time.Second, "nvidia-smi",
		"--query-gpu=name,utilization.gpu,temperature.gpu,memory.used,memory.total", "--format=csv,noheader,nounits")
	if code != 0 {
		return nil
	}
	return ParsearNvidiaSMI(ejec.Stdout(out))
}

// ParsearNvidiaSMI interpreta la primera línea de nvidia-smi en CSV.
func ParsearNvidiaSMI(txt string) *GPU {
	linea := strings.TrimSpace(strings.SplitN(txt, "\n", 2)[0])
	p := strings.Split(linea, ",")
	if len(p) < 5 {
		return nil
	}
	num := func(s string) float64 { f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64); return f }
	return &GPU{Nombre: strings.TrimSpace(p[0]), UsoPct: num(p[1]), TempC: num(p[2]), VRAMUsoMB: num(p[3]), VRAMTotMB: num(p[4])}
}

// LeerEntrada lista teclados y ratones presentes y los que tienen problemas.
func LeerEntrada() *Entrada {
	if !ejec.EsWindows() {
		return nil
	}
	_, out := ejec.PS(30*time.Second, "Get-PnpDevice -Class Keyboard,Mouse -PresentOnly | Select @{n='Status';e={\"$($_.Status)\"}},Class,FriendlyName | ConvertTo-Json")
	return ParsearEntrada(ejec.Stdout(out))
}

// ParsearEntrada interpreta la salida JSON de Get-PnpDevice.
func ParsearEntrada(txt string) *Entrada {
	e := &Entrada{Leido: time.Now(), Problemas: []string{}}
	var v any
	if json.Unmarshal([]byte(strings.TrimPrefix(txt, "\ufeff")), &v) != nil {
		return e
	}
	var lista []any
	switch t := v.(type) {
	case []any:
		lista = t
	case map[string]any:
		lista = []any{t}
	}
	for _, x := range lista {
		m, ok := x.(map[string]any)
		if !ok {
			continue
		}
		clase, _ := m["Class"].(string)
		estado, _ := m["Status"].(string)
		nombre, _ := m["FriendlyName"].(string)
		switch clase {
		case "Keyboard":
			e.Teclados++
		case "Mouse":
			e.Ratones++
		}
		if estado != "OK" && estado != "" {
			e.Problemas = append(e.Problemas, fmt.Sprintf("%s (%s)", nombre, estado))
		}
	}
	return e
}
