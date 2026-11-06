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
	datosEco      = []byte("playbox-sonda")
	tamRespuesta  = 28 + 8 + len(datosEco) + 8 // ICMP_ECHO_REPLY (64 bits) + datos + margen
	invalidHandle = ^uintptr(0)
	estadosICMP   = map[uint32]string{11003: "destino inalcanzable", 11010: "tiempo agotado", 11013: "TTL expirado"}
)

type icmpWin struct {
	mu sync.Mutex
	h  uintptr
}

// Nueva crea una sonda ICMP nativa de Windows.
func Nueva() (Sonda, error) {
	h, _, err := procCreate.Call()
	if h == invalidHandle || h == 0 {
		return nil, fmt.Errorf("IcmpCreateFile: %v", err)
	}
	return &icmpWin{h: h}, nil
}

func (s *icmpWin) Eco(ip string, espera time.Duration) Resultado {
	dst := net.ParseIP(ip).To4()
	if dst == nil {
		return Resultado{Err: "IP no válida"}
	}
	addr := uint32(dst[0]) | uint32(dst[1])<<8 | uint32(dst[2])<<16 | uint32(dst[3])<<24
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
	return Resultado{OK: true, RTT: time.Duration(rtt) * time.Millisecond}
}

func (s *icmpWin) Cerrar() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.h != 0 {
		_, _, _ = procClose.Call(s.h)
		s.h = 0
	}
}
