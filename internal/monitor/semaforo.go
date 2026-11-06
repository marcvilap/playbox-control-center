package monitor

import "fmt"

// Semaforo resume si el centro está listo para una sesión.
type Semaforo struct {
	Color   string   `json:"color"` // verde, ambar, rojo, gris
	Titulo  string   `json:"titulo"`
	Motivos []string `json:"motivos"`
}

func calcularSemaforo(g EstadoGlobal, cfg Config) Semaforo {
	var rojos, ambar []string
	configuradas, listas := 0, 0
	for _, v := range g.Visores {
		if v.Estado == SinConfig {
			continue
		}
		configuradas++
		switch v.Estado {
		case Offline:
			rojos = append(rojos, v.ID+" no responde")
			continue
		case SinConf:
			ambar = append(ambar, v.ID+" aún sin señal")
			continue
		}
		ok := true
		if v.LatenciaAlta {
			ambar = append(ambar, v.ID+" con latencia alta")
			ok = false
		}
		if t := v.Telemetria; t != nil {
			switch {
			case t.Error != "":
				ambar = append(ambar, v.ID+": sin datos de batería")
			case t.BateriaPct != nil && *t.BateriaPct < 15 && !t.Cargando:
				rojos = append(rojos, fmt.Sprintf("%s con batería al %d %%", v.ID, *t.BateriaPct))
				ok = false
			case t.BateriaPct != nil && *t.BateriaPct < cfg.BateriaBajaPct && !t.Cargando:
				ambar = append(ambar, fmt.Sprintf("%s con batería al %d %%", v.ID, *t.BateriaPct))
				ok = false
			}
			if t.Error == "" && t.TempC != nil && *t.TempC >= float64(cfg.TempAltaC) {
				ambar = append(ambar, fmt.Sprintf("%s caliente (%.0f °C)", v.ID, *t.TempC))
				ok = false
			}
			if t.Error == "" && t.Pantalla == "Reposo" {
				ambar = append(ambar, v.ID+" en reposo")
				ok = false
			}
		}
		if ok {
			listas++
		}
	}
	if g.Router.Estado == Offline {
		rojos = append(rojos, "El router no responde")
	}
	if g.Internet.Estado == Offline {
		ambar = append(ambar, "Sin Internet (la red local puede funcionar)")
	}
	if p := g.PC; p != nil {
		if e := p.Entrada; e != nil && (len(e.Problemas) > 0 || e.Teclados == 0 || e.Ratones == 0) {
			rojos = append(rojos, "Teclado o ratón del PC con problemas")
		}
		if p.CPUPct > 90 {
			ambar = append(ambar, fmt.Sprintf("CPU del PC al %.0f %%", p.CPUPct))
		}
		if p.RAMPct > 90 {
			ambar = append(ambar, fmt.Sprintf("RAM del PC al %.0f %%", p.RAMPct))
		}
		if p.DiscoLibreGB > 0 && p.DiscoLibreGB < 10 {
			ambar = append(ambar, fmt.Sprintf("Disco casi lleno (%.0f GB libres)", p.DiscoLibreGB))
		}
		if p.GPU != nil && p.GPU.TempC >= 85 {
			ambar = append(ambar, fmt.Sprintf("Gráfica caliente (%.0f °C)", p.GPU.TempC))
		}
	}
	switch {
	case configuradas == 0:
		return Semaforo{Color: "gris", Titulo: "Sin gafas configuradas", Motivos: []string{"Vincula las gafas para ver si el centro está listo"}}
	case len(rojos) > 0:
		return Semaforo{Color: "rojo", Titulo: "No listo para la sesión", Motivos: append(rojos, ambar...)}
	case len(ambar) > 0:
		return Semaforo{Color: "ambar", Titulo: "Listo con avisos", Motivos: ambar}
	}
	return Semaforo{Color: "verde", Titulo: "Listo para la sesión",
		Motivos: []string{fmt.Sprintf("%d de %d gafas conectadas y bien; router e Internet correctos", listas, configuradas)}}
}
