package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"playbox/internal/monitor"
	"playbox/internal/sonda"
)

func dirExe() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	return "."
}

func rutaConfig() string { return filepath.Join(dirExe(), "playbox.json") }
func dirDatos() string   { return filepath.Join(dirExe(), "datos") }

// cargarConfig lee playbox.json; si no existía, crea la plantilla y lo explica.
func cargarConfig() (monitor.Config, bool) {
	c, nueva, err := monitor.Cargar(rutaConfig())
	if err != nil {
		fmt.Println("Error en la configuración:", err)
		return c, false
	}
	if nueva {
		fmt.Printf("He creado %s con 10 visores sin IP.\n", rutaConfig())
	}
	if c.VisoresConIP() == 0 {
		fmt.Println("Aviso: ningún visor tiene IP en playbox.json; solo se vigilarán router e Internet.")
		fmt.Println("Ábrelo con el Bloc de notas y rellena \"ip\" de cada PICO (mira el informe F0 o la lista de clientes del router).")
	}
	return c, true
}

func configSimulacion() monitor.Config {
	c := monitor.PorDefecto()
	for i := range c.Visores {
		c.Visores[i].IP = fmt.Sprintf("%s%d", sonda.PrefijoSimulado, i+1)
	}
	c.Router, c.Internet = sonda.RouterSimulado, sonda.InternetSimulado
	c.VentanaAntesMin, c.VentanaDespuesMin = 2, 1
	return c
}

// cmdVigilar ejecuta la vigilancia en esta ventana hasta Ctrl+C.
func cmdVigilar(simular bool) int {
	var cfg monitor.Config
	dir := dirDatos()
	if simular {
		cfg, dir = configSimulacion(), filepath.Join(dirExe(), "datos-simulacion")
	} else {
		var ok bool
		if cfg, ok = cargarConfig(); !ok {
			return 1
		}
	}
	var ultimas []string
	log := func(s string) {
		ultimas = append(ultimas, time.Now().Format("15:04:05")+"  "+s)
		if len(ultimas) > 6 {
			ultimas = ultimas[len(ultimas)-6:]
		}
	}
	mon, err := monitor.Nuevo(cfg, dir, Version, log)
	if err != nil {
		fmt.Println("Error:", err)
		return 1
	}
	if simular {
		mon.NuevaSonda = sonda.NuevaSimulada(time.Now())
	}
	ctx, parar := signal.NotifyContext(context.Background(), os.Interrupt)
	defer parar()
	fin := make(chan error, 1)
	go func() { fin <- mon.Ejecutar(ctx) }()

	titulo := "VIGILANCIA"
	if simular {
		titulo = "SIMULACIÓN (visores virtuales)"
	}
	t := time.NewTicker(time.Second)
	defer t.Stop()
	inicio := time.Now()
	for {
		select {
		case err := <-fin:
			if err != nil {
				fmt.Println("Error:", err)
				return 1
			}
			fmt.Println("\nVigilancia detenida. Datos en", dir)
			return 0
		case <-t.C:
			cab := fmt.Sprintf("PlayBox %s · %s · %s · Ctrl+C para salir", Version, titulo, time.Since(inicio).Round(time.Second))
			if simular {
				cab += "\nGuion: PICO-03 cae a los 30 s · PICO-07 latencia alta a los 45 s · Internet cae a los 100 s"
			}
			pintar(cab, mon.Instantanea(), mon.Caja().Recientes(8), dir)
		}
	}
}

// cmdEstado muestra el estado que escribe el servicio en datos\estado.json.
func cmdEstado() int {
	datos, err := os.ReadFile(filepath.Join(dirDatos(), "estado.json"))
	if err != nil {
		fmt.Println("No hay estado: el servicio de vigilancia no se ha ejecutado todavía en esta carpeta.")
		return 1
	}
	var g monitor.EstadoGlobal
	if err := json.Unmarshal(datos, &g); err != nil {
		fmt.Println("estado.json ilegible:", err)
		return 1
	}
	cab := fmt.Sprintf("PlayBox · estado del servicio (v%s)", g.Version)
	if edad := time.Since(g.Actualizado); edad > time.Duration(3*g.IntervaloS+5)*time.Second {
		cab += fmt.Sprintf("\nATENCIÓN: datos de hace %s; el servicio no parece estar en marcha.", hace(g.Actualizado))
	}
	pintar(cab, g, nil, dirDatos())
	return 0
}

func hace(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d s", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h", int(d.Hours()))
	}
	return t.Format("02/01 15:04")
}

func fila(e monitor.EstadoEquipo) string {
	rtt, ultimo, jit := "—", "—", "—"
	if e.RTTms != nil {
		rtt = fmt.Sprintf("%.0f ms", *e.RTTms)
	}
	if e.UltimoContacto != nil {
		ultimo = "hace " + hace(*e.UltimoContacto)
	}
	if e.JitterMs != nil {
		jit = fmt.Sprintf("%.0f ms", *e.JitterMs)
	}
	estado := e.Estado
	if e.LatenciaAlta {
		estado += " ⚠ lento"
	}
	if e.Estado == monitor.SinConfig {
		return fmt.Sprintf("  %-9s %-16s %-24s", e.ID, "—", estado)
	}
	return fmt.Sprintf("  %-9s %-16s %-24s %-8s %-7s %-7s %-13s desde hace %s",
		e.ID, e.IP, estado, rtt, jit, fmt.Sprintf("%.0f%%", e.PerdidaPct), ultimo, hace(e.Desde))
}

func pintar(cabecera string, g monitor.EstadoGlobal, eventos []monitor.Evento, dir string) {
	var b strings.Builder
	b.WriteString("\033[H\033[2J") // limpiar pantalla
	b.WriteString(cabecera + "\n\n")
	b.WriteString(fmt.Sprintf("  %-9s %-16s %-24s %-8s %-7s %-7s %-13s %s\n", "EQUIPO", "IP", "ESTADO", "LATENC.", "JITTER", "PÉRDIDA", "ÚLTIMA SEÑAL", "CAMBIO"))
	for _, v := range g.Visores {
		b.WriteString(fila(v) + "\n")
	}
	b.WriteString("\n" + fila(g.Router) + "\n" + fila(g.Internet) + "\n\n")
	b.WriteString(fmt.Sprintf("  Visores activos: %d · Incidentes hoy: %d · Fuente: ping ICMP (observado)\n", g.VisoresOnline, g.IncidentesHoy))
	b.WriteString("  ONLINE = el visor responde en la red; no confirma que la app o la partida estén abiertas.\n")
	if len(eventos) > 0 {
		b.WriteString("\n  Últimos eventos:\n")
		for _, e := range eventos {
			b.WriteString(fmt.Sprintf("  %s  %-7s %s\n", e.TS.Local().Format("15:04:05"), e.Severidad, e.Mensaje))
		}
	}
	b.WriteString("\n  Datos y expedientes: " + dir + "\n")
	fmt.Print(b.String())
}
