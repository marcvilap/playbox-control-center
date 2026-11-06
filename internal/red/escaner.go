// Package red busca dispositivos en la red local del PC (para encontrar
// los visores sin teclear IPs). Solo lee: ping/ARP y nombres DNS.
package red

import (
	"context"
	"encoding/binary"
	"net"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"playbox/internal/ejec"
	"playbox/internal/sonda"
)

// Dispositivo encontrado en la red.
type Dispositivo struct {
	IP           string   `json:"ip"`
	Nombre       string   `json:"nombre"`
	MAC          string   `json:"mac"`
	RTTms        *float64 `json:"rtt_ms"`
	Via          string   `json:"via"` // ICMP, ARP o "tabla ARP"
	EsEstePC     bool     `json:"es_este_pc"`
	EsRouter     bool     `json:"es_router"`
	PosibleVisor bool     `json:"posible_visor"`
	Nuevo        bool     `json:"nuevo"`    // apareció al encender las gafas (paso 2)
	Conocido     string   `json:"conocido"` // visor ya asignado con esa MAC o IP
}

// Clave identifica un dispositivo entre búsquedas (MAC si se conoce, si no IP).
func (d Dispositivo) Clave() string {
	if d.MAC != "" {
		return d.MAC
	}
	return d.IP
}

// Activo indica si respondió de verdad en esta búsqueda (no solo caché ARP).
func (d Dispositivo) Activo() bool { return d.Via == "ICMP" || d.Via == "ARP" }

// Subred analizada.
type Subred struct {
	Interfaz string `json:"interfaz"`
	Red      string `json:"red"`
	MiIP     string `json:"mi_ip"`
}

// Resultado del escaneo.
type Resultado struct {
	Subredes     []Subred      `json:"subredes"`
	Dispositivos []Dispositivo `json:"dispositivos"`
	Duracion     float64       `json:"duracion_s"`
}

const maxHosts = 1024

func subredesLocales() ([]Subred, []*net.IPNet, map[string]bool) {
	var subs []Subred
	var redes []*net.IPNet
	mias := map[string]bool{}
	ifaces, _ := net.Interfaces()
	for _, i := range ifaces {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok || n.IP.To4() == nil || !n.IP.IsPrivate() {
				continue
			}
			mias[n.IP.String()] = true
			ones, bits := n.Mask.Size()
			if bits-ones > 10 { // red demasiado grande: solo su /24
				n = &net.IPNet{IP: n.IP.Mask(net.CIDRMask(24, 32)), Mask: net.CIDRMask(24, 32)}
			}
			red := &net.IPNet{IP: n.IP.Mask(n.Mask), Mask: n.Mask}
			subs = append(subs, Subred{Interfaz: i.Name, Red: red.String(), MiIP: a.(*net.IPNet).IP.String()})
			redes = append(redes, red)
		}
	}
	return subs, redes, mias
}

func hosts(n *net.IPNet) []string {
	base := binary.BigEndian.Uint32(n.IP.To4())
	ones, _ := n.Mask.Size()
	total := uint32(1) << (32 - ones)
	var out []string
	for i := uint32(1); i+1 < total && len(out) < maxHosts; i++ {
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, base+i)
		out = append(out, ip.String())
	}
	return out
}

var reARP = regexp.MustCompile(`(?i)(\d+\.\d+\.\d+\.\d+)\s+([0-9a-f]{2}(?:[-:][0-9a-f]{2}){5})`)

// TablaARP lee la caché ARP del sistema (ip → mac).
func TablaARP() map[string]string {
	_, out := ejec.Run(15*time.Second, "arp", "-a")
	t := map[string]string{}
	for _, m := range reARP.FindAllStringSubmatch(out, -1) {
		mac := strings.ToLower(strings.ReplaceAll(m[2], "-", ":"))
		if mac == "ff:ff:ff:ff:ff:ff" || strings.HasPrefix(mac, "01:00:5e") || mac == "00:00:00:00:00:00" {
			continue
		}
		t[m[1]] = mac
	}
	return t
}

var pistasVisor = []string{"pico", "android", "quest", "oculus", "vr"}

func posibleVisor(nombre string) bool {
	n := strings.ToLower(nombre)
	for _, p := range pistasVisor {
		if strings.Contains(n, p) {
			return true
		}
	}
	return false
}

// Escanear recorre las subredes privadas del PC con ping (y ARP en Windows),
// cruza con la tabla ARP y resuelve nombres. Tarda entre 5 y 20 segundos.
func Escanear(ctx context.Context, nueva func() (sonda.Sonda, error), router string) (*Resultado, error) {
	t0 := time.Now()
	subs, redes, mias := subredesLocales()
	res := &Resultado{Subredes: subs, Dispositivos: []Dispositivo{}}
	if res.Subredes == nil {
		res.Subredes = []Subred{}
	}
	var objetivos []string
	for _, n := range redes {
		objetivos = append(objetivos, hosts(n)...)
	}

	encontrados := map[string]*Dispositivo{}
	var mu sync.Mutex
	trabajo := make(chan string)
	var wg sync.WaitGroup
	for w := 0; w < 64; w++ {
		s, err := nueva()
		if err != nil {
			return nil, err
		}
		wg.Add(1)
		go func(s sonda.Sonda) {
			defer wg.Done()
			defer s.Cerrar()
			for ip := range trabajo {
				r := s.Eco(ip, 700*time.Millisecond)
				if !r.OK {
					continue
				}
				ms := float64(r.RTT.Microseconds()) / 1000
				d := &Dispositivo{IP: ip, Via: r.Via, MAC: r.MAC}
				if r.Via == "ICMP" {
					d.RTTms = &ms
				}
				mu.Lock()
				encontrados[ip] = d
				mu.Unlock()
			}
		}(s)
	}
	for _, ip := range objetivos {
		select {
		case <-ctx.Done():
		case trabajo <- ip:
			continue
		}
		break
	}
	close(trabajo)
	wg.Wait()

	// Los que no contestan a nada pero el sistema acaba de ver en la red.
	for ip, mac := range TablaARP() {
		dentro := false
		for _, n := range redes {
			if n.Contains(net.ParseIP(ip)) {
				dentro = true
			}
		}
		if !dentro {
			continue
		}
		if d, ok := encontrados[ip]; ok {
			if d.MAC == "" {
				d.MAC = mac
			}
		} else {
			encontrados[ip] = &Dispositivo{IP: ip, MAC: mac, Via: "tabla ARP"}
		}
	}
	for ip := range mias {
		if _, ok := encontrados[ip]; !ok {
			for _, n := range redes {
				if n.Contains(net.ParseIP(ip)) {
					encontrados[ip] = &Dispositivo{IP: ip, Via: "este PC"}
				}
			}
		}
	}

	// Nombres por DNS inverso (el router suele conocer el nombre del dispositivo).
	resolver := &net.Resolver{}
	var wn sync.WaitGroup
	lim := make(chan struct{}, 32)
	for _, d := range encontrados {
		wn.Add(1)
		go func(d *Dispositivo) {
			defer wn.Done()
			lim <- struct{}{}
			defer func() { <-lim }()
			c, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
			defer cancel()
			if nombres, err := resolver.LookupAddr(c, d.IP); err == nil && len(nombres) > 0 {
				d.Nombre = strings.TrimSuffix(nombres[0], ".")
			}
		}(d)
	}
	wn.Wait()

	for _, d := range encontrados {
		d.EsEstePC = mias[d.IP]
		d.EsRouter = d.IP == router
		d.PosibleVisor = !d.EsEstePC && !d.EsRouter && posibleVisor(d.Nombre)
		res.Dispositivos = append(res.Dispositivos, *d)
	}
	sort.Slice(res.Dispositivos, func(i, j int) bool {
		a, b := res.Dispositivos[i], res.Dispositivos[j]
		if a.PosibleVisor != b.PosibleVisor {
			return a.PosibleVisor
		}
		return binary.BigEndian.Uint32(net.ParseIP(a.IP).To4()) < binary.BigEndian.Uint32(net.ParseIP(b.IP).To4())
	})
	res.Duracion = time.Since(t0).Round(100 * time.Millisecond).Seconds()
	return res, nil
}
