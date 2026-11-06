package descubrir

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"playbox/internal/ejec"
)

// Patron identifica el software de los tres proveedores.
const Patron = "spawn|vex|hero"

const proc = "@{n='Proc';e={(Get-Process -Id $_.OwningProcess).Name}}"

var scriptsProveedores = []script{
	{"servicios", "Get-Service | ? { $_.DisplayName -match '" + Patron + "' -or $_.Name -match '" + Patron + "' } | Select @{n='Status';e={\"$($_.Status)\"}},Name,DisplayName,@{n='Inicio';e={\"$($_.StartType)\"}} | ConvertTo-Json"},
	{"procesos", "Get-Process | ? { $_.Path -match '" + Patron + "' -or $_.Name -match '" + Patron + "' } | Select Name,Id,Path,@{n='Inicio';e={$_.StartTime.ToString('s')}},CPU,WorkingSet64 | ConvertTo-Json"},
	{"puertos_tcp", "Get-NetTCPConnection -State Listen | Select LocalAddress,LocalPort,OwningProcess," + proc + " | ConvertTo-Json"},
	{"puertos_udp", "Get-NetUDPEndpoint | Select LocalAddress,LocalPort,OwningProcess," + proc + " | ConvertTo-Json"},
	{"conexiones_tcp", "Get-NetTCPConnection -State Established | Select LocalPort,RemoteAddress,RemotePort,OwningProcess," + proc + " | ConvertTo-Json"},
	{"carpetas", "Get-ChildItem $env:ProgramData,$env:LOCALAPPDATA,$env:APPDATA,\"$env:USERPROFILE\\AppData\\LocalLow\",'C:\\Program Files','C:\\Program Files (x86)' -Directory | ? Name -match '" + Patron + "' | Select -Expand FullName | ConvertTo-Json"},
}

// ResumenProveedores resume el software de VEX/HeroZone/Spawnpoint en el PC.
type ResumenProveedores struct {
	Servicios    []string `json:"servicios"`
	Procesos     []string `json:"procesos"`
	Puertos      []string `json:"puertos"`
	Carpetas     []string `json:"carpetas"`
	LogsCopiados int      `json:"logs_copiados"`
}

const (
	maxLogs   = 40
	colaBytes = 200_000
	diasLog   = 14
	profMax   = 5
)

var extLog = map[string]bool{".log": true, ".txt": true, ".json": true}

type logCopiado struct {
	Origen     string `json:"origen"`
	Bytes      int64  `json:"bytes"`
	Modificado string `json:"modificado"`
}

func copiarColasLogs(carpetas []string, out string) int {
	limite := time.Now().Add(-diasLog * 24 * time.Hour)
	type cand struct {
		ruta string
		info fs.FileInfo
	}
	var cands []cand
	for _, base := range carpetas {
		_ = filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(base, p)
			if d.IsDir() {
				if strings.Count(rel, string(filepath.Separator)) >= profMax {
					return filepath.SkipDir
				}
				return nil
			}
			if !extLog[strings.ToLower(filepath.Ext(p))] {
				return nil
			}
			if info, e := d.Info(); e == nil && info.ModTime().After(limite) {
				cands = append(cands, cand{p, info})
			}
			return nil
		})
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].info.ModTime().After(cands[j].info.ModTime()) })
	dest := filepath.Join(out, "2_proveedores", "logs")
	_ = os.MkdirAll(dest, 0o755)
	var copiados []logCopiado
	for i, c := range cands {
		if i == maxLogs {
			break
		}
		f, err := os.Open(c.ruta) // solo lectura
		if err != nil {
			continue
		}
		if off := c.info.Size() - colaBytes; off > 0 {
			_, _ = f.Seek(off, io.SeekStart)
		}
		datos, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			continue
		}
		nombre := fmt.Sprintf("%02d_%s", i, nombreSeguro(filepath.Base(c.ruta)))
		_ = os.WriteFile(filepath.Join(dest, nombre), datos, 0o644)
		copiados = append(copiados, logCopiado{c.ruta, c.info.Size(), c.info.ModTime().Format("2006-01-02T15:04:05")})
	}
	j, _ := json.MarshalIndent(copiados, "", "  ")
	_ = os.WriteFile(filepath.Join(dest, "_indice.json"), j, 0o644)
	return len(copiados)
}

func recogerProveedores(out string, conLogs bool) ResumenProveedores {
	dir := filepath.Join(out, "2_proveedores")
	res := map[string]string{}
	for _, s := range scriptsProveedores {
		_, txt := ejec.PS(2*time.Minute, s.ps)
		guardar(dir, s.nombre, txt)
		res[s.nombre] = txt
	}
	var r ResumenProveedores
	for _, s := range ComoLista(res["servicios"]) {
		r.Servicios = append(r.Servicios, fmt.Sprintf("%s [%s, %s]", S(s, "Name"), S(s, "Status"), S(s, "Inicio")))
	}
	pids := map[string]bool{}
	for _, p := range ComoLista(res["procesos"]) {
		pids[S(p, "Id")] = true
		r.Procesos = append(r.Procesos, fmt.Sprintf("%s (pid %s) %s", S(p, "Name"), S(p, "Id"), S(p, "Path")))
	}
	vistos := map[string]bool{}
	for _, t := range []struct{ k, proto string }{{"puertos_tcp", "TCP"}, {"puertos_udp", "UDP"}} {
		for _, p := range ComoLista(res[t.k]) {
			if pids[S(p, "OwningProcess")] {
				x := fmt.Sprintf("%s %s %s", S(p, "Proc"), t.proto, S(p, "LocalPort"))
				if !vistos[x] {
					vistos[x] = true
					r.Puertos = append(r.Puertos, x)
				}
			}
		}
	}
	sort.Strings(r.Puertos)
	r.Carpetas = ComoTextos(res["carpetas"])
	if conLogs {
		r.LogsCopiados = copiarColasLogs(r.Carpetas, out)
	}
	return r
}
