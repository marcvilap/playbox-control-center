//go:build !windows

package sonda

import (
	"regexp"
	"strconv"
	"time"

	"playbox/internal/ejec"
)

var reTime = regexp.MustCompile(`time[=<]([\d.]+)`)

type pingProc struct{}

// Nueva crea una sonda basada en el comando ping (solo para desarrollo).
func Nueva() (Sonda, error) { return pingProc{}, nil }

func (pingProc) Eco(ip string, espera time.Duration) Resultado {
	seg := int(espera.Seconds())
	if seg < 1 {
		seg = 1
	}
	code, out := ejec.Run(espera+2*time.Second, "ping", "-c", "1", "-W", strconv.Itoa(seg), ip)
	if code != 0 {
		return Resultado{Err: "sin respuesta"}
	}
	if m := reTime.FindStringSubmatch(out); m != nil {
		ms, _ := strconv.ParseFloat(m[1], 64)
		return Resultado{OK: true, RTT: time.Duration(ms * float64(time.Millisecond)), Via: "ICMP"}
	}
	return Resultado{OK: true, Via: "ICMP"}
}

func (pingProc) Cerrar() {}
