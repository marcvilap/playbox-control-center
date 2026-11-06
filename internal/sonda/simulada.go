package sonda

import (
	"math/rand"
	"strings"
	"time"
)

// IPs del escenario de simulación (no existen en la red real).
const (
	PrefijoSimulado  = "10.99.0."
	RouterSimulado   = "10.99.0.254"
	InternetSimulado = "10.99.1.1"
)

// Simulada responde según un guion fijo para probar sin el centro:
//   - PICO-03 deja de responder entre los segundos 30 y 75
//   - PICO-07 tiene latencia alta entre los segundos 45 y 90
//   - Internet cae entre los segundos 100 y 130 (el router sigue bien)
type Simulada struct {
	inicio time.Time
}

// NuevaSimulada crea una sonda de guion; todas comparten el mismo reloj.
func NuevaSimulada(inicio time.Time) func() (Sonda, error) {
	return func() (Sonda, error) { return &Simulada{inicio: inicio}, nil }
}

func (s *Simulada) Eco(ip string, espera time.Duration) Resultado {
	t := time.Since(s.inicio).Seconds()
	base := time.Duration(3+rand.Intn(10)) * time.Millisecond
	switch {
	case ip == PrefijoSimulado+"3" && t >= 30 && t < 75:
		time.Sleep(espera)
		return Resultado{Err: "tiempo agotado (simulado)"}
	case ip == PrefijoSimulado+"7" && t >= 45 && t < 90:
		return Resultado{OK: true, Via: "ICMP", RTT: time.Duration(110+rand.Intn(60)) * time.Millisecond}
	case ip == InternetSimulado && t >= 100 && t < 130:
		time.Sleep(espera)
		return Resultado{Err: "tiempo agotado (simulado)"}
	case ip == InternetSimulado:
		return Resultado{OK: true, Via: "ICMP", RTT: base + 15*time.Millisecond}
	case ip == RouterSimulado || strings.HasPrefix(ip, PrefijoSimulado):
		return Resultado{OK: true, Via: "ICMP", RTT: base}
	}
	return Resultado{Err: "fuera de la simulación"}
}

func (s *Simulada) Cerrar() {}
