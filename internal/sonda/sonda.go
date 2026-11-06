// Package sonda mide si un equipo responde en la red (ICMP echo).
//
// En Windows usa la API IcmpSendEcho de iphlpapi.dll: no lanza procesos,
// no necesita permisos de administrador y apenas consume CPU.
package sonda

import "time"

// Resultado de un único eco.
type Resultado struct {
	OK  bool
	RTT time.Duration
	Err string
	Via string // ICMP o ARP
	MAC string // solo si respondió por ARP
}

// Sonda envía ecos ICMP.
type Sonda interface {
	Eco(ip string, espera time.Duration) Resultado
	Cerrar()
}
