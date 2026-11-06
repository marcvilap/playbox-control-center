//go:build windows

package pc

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32                = windows.NewLazySystemDLL("kernel32.dll")
	procSystemTimes         = kernel32.NewProc("GetSystemTimes")
	procMemoryStatus        = kernel32.NewProc("GlobalMemoryStatusEx")
	ultimoIdle, ultimoTotal uint64
)

type memoryStatusEx struct {
	Length, MemoryLoad                     uint32
	TotalPhys, AvailPhys                   uint64
	TotalPageFile, AvailPageFile           uint64
	TotalVirtual, AvailVirtual, AvailExtVr uint64
}

func ft(f windows.Filetime) uint64 { return uint64(f.HighDateTime)<<32 | uint64(f.LowDateTime) }

// cpu devuelve el uso de CPU desde la llamada anterior (la primera da 0).
func cpu() float64 {
	var idle, kernel, user windows.Filetime
	r, _, _ := procSystemTimes.Call(uintptr(unsafe.Pointer(&idle)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
	if r == 0 {
		return 0
	}
	i, total := ft(idle), ft(kernel)+ft(user) // kernel incluye idle
	di, dt := i-ultimoIdle, total-ultimoTotal
	primera := ultimoTotal == 0
	ultimoIdle, ultimoTotal = i, total
	if primera || dt == 0 {
		return 0
	}
	return float64(dt-di) / float64(dt) * 100
}

// Leer mide CPU, RAM y disco del sistema (llamadas nativas, sin procesos).
func Leer() Salud {
	s := Salud{CPUPct: redondear(cpu()), Leido: time.Now()}
	m := memoryStatusEx{}
	m.Length = uint32(unsafe.Sizeof(m))
	if r, _, _ := procMemoryStatus.Call(uintptr(unsafe.Pointer(&m))); r != 0 && m.TotalPhys > 0 {
		s.RAMPct = redondear(float64(m.TotalPhys-m.AvailPhys) / float64(m.TotalPhys) * 100)
		s.RAMTotalGB = redondear(float64(m.TotalPhys) / (1 << 30))
	}
	var libre, total, libreTotal uint64
	raiz, _ := windows.UTF16PtrFromString(`C:\`)
	if windows.GetDiskFreeSpaceEx(raiz, &libre, &total, &libreTotal) == nil && total > 0 {
		s.DiscoLibreGB = redondear(float64(libre) / (1 << 30))
		s.DiscoPct = redondear(float64(total-libre) / float64(total) * 100)
	}
	return s
}
