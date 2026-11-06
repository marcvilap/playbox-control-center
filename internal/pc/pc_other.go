//go:build !windows

package pc

import (
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var ultimoIdle, ultimoTotal uint64

func cpu() float64 {
	datos, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}
	campos := strings.Fields(strings.SplitN(string(datos), "\n", 2)[0])[1:]
	var total, idle uint64
	for i, c := range campos {
		v, _ := strconv.ParseUint(c, 10, 64)
		total += v
		if i == 3 || i == 4 {
			idle += v
		}
	}
	di, dt := idle-ultimoIdle, total-ultimoTotal
	primera := ultimoTotal == 0
	ultimoIdle, ultimoTotal = idle, total
	if primera || dt == 0 {
		return 0
	}
	return float64(dt-di) / float64(dt) * 100
}

// Leer mide CPU, RAM y disco (versión de desarrollo para Linux).
func Leer() Salud {
	s := Salud{CPUPct: redondear(cpu()), Leido: time.Now()}
	if datos, err := os.ReadFile("/proc/meminfo"); err == nil {
		var tot, disp float64
		for _, l := range strings.Split(string(datos), "\n") {
			f := strings.Fields(l)
			if len(f) < 2 {
				continue
			}
			v, _ := strconv.ParseFloat(f[1], 64)
			switch f[0] {
			case "MemTotal:":
				tot = v
			case "MemAvailable:":
				disp = v
			}
		}
		if tot > 0 {
			s.RAMPct = redondear((tot - disp) / tot * 100)
			s.RAMTotalGB = redondear(tot / (1 << 20))
		}
	}
	var st syscall.Statfs_t
	if syscall.Statfs("/", &st) == nil && st.Blocks > 0 {
		libre := float64(st.Bavail) * float64(st.Bsize)
		total := float64(st.Blocks) * float64(st.Bsize)
		s.DiscoLibreGB = redondear(libre / (1 << 30))
		s.DiscoPct = redondear((total - libre) / total * 100)
	}
	return s
}
