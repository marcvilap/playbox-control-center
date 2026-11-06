package descubrir

import (
	"math"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"playbox/internal/ejec"
)

// Ping es el resultado de medir un destino.
type Ping struct {
	IP         string   `json:"ip"`
	Enviados   int      `json:"enviados"`
	PerdidaPct string   `json:"perdida_pct"`
	MinMs      string   `json:"min_ms"`
	MediaMs    string   `json:"media_ms"`
	MaxMs      string   `json:"max_ms"`
	JitterMs   *float64 `json:"jitter_ms"`
}

// ResumenRed agrupa puerta de enlace y mediciones.
type ResumenRed struct {
	Gateway string `json:"gateway"`
	Pings   []Ping `json:"pings"`
}

var (
	rePerdidaWin = regexp.MustCompile(`\((\d+)%`)
	rePerdidaNix = regexp.MustCompile(`(\d+(?:\.\d+)?)% packet loss`)
	reTiempoWin  = regexp.MustCompile(`(?i)(?:tiempo|time)[=<](\d+)\s*m`)
	reResumenWin = regexp.MustCompile(`=\s*(\d+)\s*ms`)
	reTiempoNix  = regexp.MustCompile(`time=([\d.]+)`)
	reRTTNix     = regexp.MustCompile(`= ([\d.]+)/([\d.]+)/([\d.]+)/`)
)

// ParsearPing interpreta la salida de ping (Windows en español/inglés o Linux).
func ParsearPing(ip string, n int, txt string, windows bool) Ping {
	p := Ping{IP: ip, Enviados: n}
	var tiempos []string
	if windows {
		p.PerdidaPct = Buscar(rePerdidaWin, txt)
		for _, m := range reTiempoWin.FindAllStringSubmatch(txt, -1) {
			tiempos = append(tiempos, m[1])
		}
		// Resumen: "Mínimo = 1ms, Máximo = 3ms, Media = 2ms" (mín, máx, media)
		for _, l := range strings.Split(txt, "\n") {
			if m := reResumenWin.FindAllStringSubmatch(l, -1); len(m) == 3 {
				p.MinMs, p.MaxMs, p.MediaMs = m[0][1], m[1][1], m[2][1]
			}
		}
	} else {
		p.PerdidaPct = Buscar(rePerdidaNix, txt)
		for _, m := range reTiempoNix.FindAllStringSubmatch(txt, -1) {
			tiempos = append(tiempos, m[1])
		}
		if m := reRTTNix.FindStringSubmatch(txt); m != nil {
			p.MinMs, p.MediaMs, p.MaxMs = m[1], m[2], m[3]
		}
	}
	if len(tiempos) > 1 {
		var suma float64
		ant, _ := strconv.ParseFloat(tiempos[0], 64)
		for _, t := range tiempos[1:] {
			v, _ := strconv.ParseFloat(t, 64)
			suma += math.Abs(v - ant)
			ant = v
		}
		j := math.Round(suma/float64(len(tiempos)-1)*10) / 10
		p.JitterMs = &j
	}
	return p
}

func hacerPing(ip string, n int) (Ping, string) {
	var txt string
	espera := time.Duration(n*2+30) * time.Second
	if ejec.EsWindows() {
		_, txt = ejec.Run(espera, "ping", "-n", strconv.Itoa(n), ip)
	} else {
		_, txt = ejec.Run(espera, "ping", "-c", strconv.Itoa(n), "-i", "1", ip)
	}
	return ParsearPing(ip, n, txt, ejec.EsWindows()), txt
}

func puertaEnlace() string {
	var txt string
	if ejec.EsWindows() {
		_, txt = ejec.PS(30*time.Second, "(Get-NetRoute -DestinationPrefix '0.0.0.0/0' | Sort RouteMetric | Select -First 1).NextHop")
	} else {
		_, txt = ejec.Run(10*time.Second, "sh", "-c", "ip route | awk '/default/{print $3; exit}'")
	}
	return ejec.Stdout(txt)
}

func recogerRed(out string, ips []string, n int, hacerPings bool, aviso func(string)) ResumenRed {
	dir := filepath.Join(out, "4_red")
	r := ResumenRed{Gateway: puertaEnlace()}
	_, arp := ejec.Run(30*time.Second, "arp", "-a")
	guardar(dir, "arp", arp)
	if !hacerPings {
		return r
	}
	vistos := map[string]bool{}
	var destinos []string
	for _, ip := range append([]string{r.Gateway, "1.1.1.1"}, ips...) {
		if ip != "" && !vistos[ip] {
			vistos[ip] = true
			destinos = append(destinos, ip)
		}
	}
	aviso("   Ping a " + strconv.Itoa(len(destinos)) + " destinos en paralelo (" + strconv.Itoa(n) + " paquetes, ~" + strconv.Itoa(n) + " s)…")
	r.Pings = make([]Ping, len(destinos))
	var wg sync.WaitGroup
	limite := make(chan struct{}, 16)
	for i, ip := range destinos {
		wg.Add(1)
		go func(i int, ip string) {
			defer wg.Done()
			limite <- struct{}{}
			defer func() { <-limite }()
			p, txt := hacerPing(ip, n)
			r.Pings[i] = p
			guardar(dir, "ping_"+nombreSeguro(ip), txt)
		}(i, ip)
	}
	wg.Wait()
	return r
}
