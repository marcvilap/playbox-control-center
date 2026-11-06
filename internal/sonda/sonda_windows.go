//go:build windows

package sonda

import (
	"fmt"
	"net"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	iphlpapi      = windows.NewLazySystemDLL("iphlpapi.dll")
	procCreate    = iphlpapi.NewProc("IcmpCreateFile")
	procClose     = iphlpapi.NewProc("IcmpCloseHandle")
	procSendEcho  = iphlpapi.NewProc("IcmpSendEcho")
	procSendARP   = iphlpapi.NewProc("SendARP")
	datosEco      = []byte("playbox-sonda")
	tamRespuesta  = 28 + 8 + len(datosEco) + 8 // ICMP_ECHO_REPLY (64 bits) + datos + margen
	invalidHandle = ^uintptr(0)
	estadosICMP   = map[uint32]string{11003: "destino inalcanzable", 11010: "tiempo agotado", 11013: "TTL expirado"}
)

type icmpWin struct {
	mu  sync.Mutex
	h   uintptr
	ARP bool
}

// Nueva crea una sonda ICMP nativa de Windows.
func Nueva() (Sonda, error) {
	h, _, err := procCreate.Call()
	if h == invalidHandle || h == 0 {
		return nil, fmt.Errorf("IcmpCreateFile: %v", err)
	}
	return &icmpWin{h: h, ARP: true}, nil
}

// Eco prueba primero ICMP; si no hay respuesta, pregunta por ARP (muchos
// dispositivos Android ignoran el ping pero siempre contestan a ARP en la
// red local). ARP solo funciona dentro de la misma subred.
func (s *icmpWin) Eco(ip string, espera time.Duration) Resultado {
	dst := net.ParseIP(ip).To4()
	if dst == nil {
		return Resultado{Err: "IP no válida"}
	}
	addr := uint32(dst[0]) | uint32(dst[1])<<8 | uint32(dst[2])<<16 | uint32(dst[3])<<24
	r := s.icmp(addr, espera)
	if r.OK || !s.ARP || !EnSubredLocal(dst) {
		return r
	}
	if a := arp(addr); a.OK {
		return a
	}
	return r
}

func arp(addr uint32) Resultado {
	var mac [8]byte
	tam := uint32(len(mac))
	t0 := time.Now()
	ret, _, _ := procSendARP.Call(uintptr(addr), 0, uintptr(unsafe.Pointer(&mac[0])), uintptr(unsafe.Pointer(&tam)))
	if ret != 0 || tam == 0 {
		return Resultado{Err: "sin respuesta (ni ping ni ARP)"}
	}
	return Resultado{OK: true, RTT: time.Since(t0), Via: "ARP", MAC: net.HardwareAddr(mac[:tam]).String()}
}

func (s *icmpWin) icmp(addr uint32, espera time.Duration) Resultado {
	buf := make([]byte, tamRespuesta+64)
	s.mu.Lock() // un handle por sonda; las llamadas se serializan
	n, _, err := procSendEcho.Call(s.h, uintptr(addr),
		uintptr(unsafe.Pointer(&datosEco[0])), uintptr(len(datosEco)), 0,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(espera.Milliseconds()))
	s.mu.Unlock()
	if n == 0 {
		if en, ok := err.(windows.Errno); ok {
			if txt, ok := estadosICMP[uint32(en)]; ok {
				return Resultado{Err: txt}
			}
		}
		return Resultado{Err: "sin respuesta"}
	}
	// ICMP_ECHO_REPLY: Address uint32, Status uint32, RoundTripTime uint32, ...
	estado := *(*uint32)(unsafe.Pointer(&buf[4]))
	rtt := *(*uint32)(unsafe.Pointer(&buf[8]))
	if estado != 0 {
		if txt, ok := estadosICMP[estado]; ok {
			return Resultado{Err: txt}
		}
		return Resultado{Err: fmt.Sprintf("estado ICMP %d", estado)}
	}
	return Resultado{OK: true, RTT: time.Duration(rtt) * time.Millisecond, Via: "ICMP"}
}

func (s *icmpWin) Cerrar() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.h != 0 {
		_, _, _ = procClose.Call(s.h)
		s.h = 0
	}
}

// EnSubredLocal indica si la IP está en alguna subred de este PC (donde ARP tiene sentido).
func EnSubredLocal(ip net.IP) bool {
	ifaces, _ := net.Interfaces()
	for _, i := range ifaces {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && n.Contains(ip) {
				return true
			}
		}
	}
	return false
}
